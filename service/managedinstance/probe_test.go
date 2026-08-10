package managedinstance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/01121531/subandnew-api/model"
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

func TestProbeGenericDetectsNewAPIAndLogsInWithAccountPassword(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/status":
			writeProbeJSON(response, `{"success":true,"data":{"version":"v1.2.3","system_name":"New API","start_time":10}}`)
		case "/api/user/login":
			require.Equal(t, http.MethodPost, request.Method)
			var input map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&input))
			require.Equal(t, "admin", input["username"])
			require.Equal(t, "password", input["password"])
			http.SetCookie(response, &http.Cookie{Name: "session", Value: "remote-session", Path: "/"})
			writeProbeJSON(response, `{"success":true,"data":{"id":7,"username":"admin"}}`)
		case "/api/status/test":
			cookie, err := request.Cookie("session")
			require.NoError(t, err)
			require.Equal(t, "remote-session", cookie.Value)
			require.Equal(t, "7", request.Header.Get("New-Api-User"))
			writeProbeJSON(response, `{"success":true}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindGeneric, CredentialInput{
		AuthType: "account_password", Secret: "password", UserID: "admin",
	})
	result, err := Probe(context.Background(), instance.Id, 7)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindNewAPI, result.Kind)
	require.Equal(t, "v1.2.3", result.Version)
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

func TestProbeGenericDetectsSub2APIAndLogsInWithAccountPassword(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/status":
			http.NotFound(response, request)
		case "/health":
			writeProbeJSON(response, `{"status":"ok"}`)
		case "/api/v1/auth/login":
			var input map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&input))
			require.Equal(t, "admin@example.com", input["email"])
			require.Equal(t, "password", input["password"])
			writeProbeJSON(response, `{"code":0,"message":"success","data":{"access_token":"account-token"}}`)
		case "/api/v1/admin/system/version":
			require.Equal(t, "Bearer account-token", request.Header.Get("Authorization"))
			writeProbeJSON(response, `{"code":0,"message":"success","data":{"version":"0.1.125"}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindGeneric, CredentialInput{
		AuthType: "account_password", Secret: "password", UserID: "admin@example.com",
	})
	result, err := Probe(context.Background(), instance.Id, 7)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindSub2API, result.Kind)
	require.Equal(t, "0.1.125", result.Version)
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
	var alert model.ManagedInstanceAlert
	require.NoError(t, db.Where("instance_id = ? AND status = ?", instance.Id, model.ManagedInstanceAlertStatusOpen).First(&alert).Error)
	require.Equal(t, model.ManagedInstanceAlertTypeCredential, alert.AlertType)
	require.Equal(t, ProbeErrorAuthentication, alert.ErrorCode)
}

func TestProbeAvailabilityAlertThresholdDeduplicatesAndResolves(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	var healthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !healthy.Load() {
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		switch request.URL.Path {
		case "/api/status":
			writeProbeJSON(response, `{"success":true,"data":{"version":"v1","system_name":"New API","start_time":10}}`)
		case "/api/status/test":
			writeProbeJSON(response, `{"success":true}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "token"})

	for attempt := 1; attempt <= 2; attempt++ {
		_, err := Probe(context.Background(), instance.Id, 7)
		require.Error(t, err)
		var count int64
		require.NoError(t, db.Model(&model.ManagedInstanceAlert{}).Where("instance_id = ?", instance.Id).Count(&count).Error)
		require.Zero(t, count)
	}
	_, err := Probe(context.Background(), instance.Id, 7)
	require.Error(t, err)
	_, err = Probe(context.Background(), instance.Id, 7)
	require.Error(t, err)
	var alert model.ManagedInstanceAlert
	require.NoError(t, db.Where("instance_id = ?", instance.Id).First(&alert).Error)
	require.Equal(t, model.ManagedInstanceAlertStatusOpen, alert.Status)
	require.Equal(t, model.ManagedInstanceAlertTypeAvailability, alert.AlertType)
	require.Equal(t, 2, alert.Occurrences)

	healthy.Store(true)
	_, err = Probe(context.Background(), instance.Id, 7)
	require.NoError(t, err)
	require.NoError(t, db.First(&alert, alert.Id).Error)
	require.Equal(t, model.ManagedInstanceAlertStatusResolved, alert.Status)
	require.NotZero(t, alert.ResolvedAt)

	alerts, err := ListAlerts(AlertListFilter{InstanceID: instance.Id, Status: model.ManagedInstanceAlertStatusResolved})
	require.NoError(t, err)
	require.Equal(t, int64(1), alerts.Total)
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
