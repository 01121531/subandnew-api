package managedinstance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/stretchr/testify/require"
)

func TestProbeNewAPIUsesBearerPAT(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/status":
			writeProbeJSON(response, `{"success":true,"data":{"version":"v1.2.3","system_name":"New API","start_time":10}}`)
		case "/api/status/test":
			require.Equal(t, "Bearer pat-secret", request.Header.Get("Authorization"))
			require.Empty(t, request.Header.Get("HUICHUAN-User"))
			writeProbeJSON(response, `{"success":true,"message":"Server is running","http_stats":{}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "pat-secret"})

	result, err := Probe(context.Background(), instance.Id, 7)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindNewAPI, result.Kind)
	require.Equal(t, "v1.2.3", result.Version)
	require.Contains(t, result.Capabilities, "channels.list")
	stored, err := Get(instance.Id)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceStatusHealthy, stored.Status)
	require.Zero(t, stored.ConsecutiveFailures)
}

func TestProbeHuichuanUsesLegacyUserHeader(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/status":
			writeProbeJSON(response, `{"success":true,"data":{"version":"v1.1.0","system_name":"HUICHUAN-AI","start_time":10}}`)
		case "/api/status/test":
			require.Equal(t, "Bearer legacy-secret", request.Header.Get("Authorization"))
			require.Equal(t, "9", request.Header.Get("HUICHUAN-User"))
			writeProbeJSON(response, `{"success":true}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindHuichuan, CredentialInput{
		AuthType: "legacy_access_token", Secret: "legacy-secret", UserID: "9",
	})
	result, err := Probe(context.Background(), instance.Id, 7)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindHuichuan, result.Kind)
}

func TestProbeGenericDetectsSub2APIAndUsesAPIKey(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/status":
			http.NotFound(response, request)
		case "/health":
			writeProbeJSON(response, `{"status":"ok"}`)
		case "/api/v1/admin/system/version":
			require.Equal(t, "admin-secret", request.Header.Get("x-api-key"))
			require.Empty(t, request.Header.Get("Authorization"))
			writeProbeJSON(response, `{"code":0,"message":"success","data":{"version":"0.1.125"}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindGeneric, CredentialInput{AuthType: "admin_token", Secret: "admin-secret"})
	result, err := Probe(context.Background(), instance.Id, 7)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindSub2API, result.Kind)
	require.Equal(t, "0.1.125", result.Version)
	require.Contains(t, result.Capabilities, "accounts.list")

	stored, err := Get(instance.Id)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindSub2API, stored.Kind)
}

func TestProbeAuthenticationFailureUpdatesStateAndRedactedAudit(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/status" {
			writeProbeJSON(response, `{"success":true,"data":{"version":"v1","system_name":"New API","start_time":10}}`)
			return
		}
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "never-log-this"})

	_, err := Probe(context.Background(), instance.Id, 7)
	var probeErr *ProbeError
	require.True(t, errors.As(err, &probeErr))
	require.Equal(t, ProbeErrorAuthentication, probeErr.Code)
	stored, err := Get(instance.Id)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceStatusAuthFailed, stored.Status)
	require.Equal(t, 1, stored.ConsecutiveFailures)

	var audit model.ManagedInstanceAudit
	require.NoError(t, db.Where("instance_id = ? AND action = ?", instance.Id, "check").First(&audit).Error)
	require.Equal(t, "failed", audit.Outcome)
	require.Contains(t, audit.Details, ProbeErrorAuthentication)
	require.NotContains(t, audit.Details, "never-log-this")
}

func createProbeInstance(t *testing.T, baseURL string, kind string, credential CredentialInput) *InstanceView {
	t.Helper()
	instance, err := Create(CreateInput{
		Name: "probe-" + kind, Kind: kind, BaseURL: baseURL, Environment: "development",
		ManagementMode: model.ManagedInstanceModeObserve, TLSVerify: true, Credential: &credential, ActorID: 1,
	})
	require.NoError(t, err)
	return instance
}

func writeProbeJSON(response http.ResponseWriter, body string) {
	response.Header().Set("Content-Type", "application/json")
	_, _ = response.Write([]byte(body))
}
