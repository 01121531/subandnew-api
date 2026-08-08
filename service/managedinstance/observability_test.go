package managedinstance

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestCollectInventoryNormalizesAndRedactsRemoteRows(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/channel/", request.URL.Path)
		require.Equal(t, "Bearer inventory-secret", request.Header.Get("Authorization"))
		writeProbeJSON(response, `{"success":true,"data":{"items":[{"id":7,"name":"primary","type":"openai","group":"default","status":1,"key":"must-not-leak","password":"must-not-leak"}],"total":1}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "inventory-secret"})

	view, err := CollectInventory(context.Background(), instance.Id, "channel", "")
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceCollectionSucceeded, view.CollectionStatus)
	page, ok := view.Data.(*InventoryPage)
	require.True(t, ok)
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	require.Equal(t, int64(7), page.Items[0].ID)
	require.Equal(t, "primary", page.Items[0].Name)
	require.NotNil(t, page.Items[0].Enabled)
	require.True(t, *page.Items[0].Enabled)

	var snapshot model.ManagedInstanceSnapshot
	require.NoError(t, db.Where("instance_id = ? AND snapshot_type = ?", instance.Id, model.ManagedInstanceSnapshotTypeInventory).First(&snapshot).Error)
	require.NotContains(t, snapshot.Payload, "must-not-leak")
	require.Equal(t, model.ManagedInstanceCollectionSucceeded, snapshot.CollectionStatus)
}

func TestCollectInventoryAggregatesNewAPIPagesIntoConflictBaseline(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	requestedPages := make([]string, 0, 2)
	firstRows := make([]map[string]any, 0, 100)
	for id := 1; id <= 100; id++ {
		firstRows = append(firstRows, map[string]any{"id": id, "name": "channel"})
	}
	encodedFirstRows, err := json.Marshal(firstRows)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestedPages = append(requestedPages, request.URL.Query().Get("page"))
		require.Equal(t, "100", request.URL.Query().Get("page_size"))
		if request.URL.Query().Get("page") == "2" {
			writeProbeJSON(response, `{"success":true,"data":{"items":[{"id":101,"name":"last"}],"total":101}}`)
			return
		}
		writeProbeJSON(response, `{"success":true,"data":{"items":`+string(encodedFirstRows)+`,"total":101}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "inventory-secret"})

	view, err := CollectInventory(context.Background(), instance.Id, "channel", "")
	require.NoError(t, err)
	require.Equal(t, []string{"1", "2"}, requestedPages)
	page := view.Data.(*InventoryPage)
	require.Len(t, page.Items, 101)
	require.Empty(t, page.NextCursor)

	var snapshot model.ManagedInstanceSnapshot
	require.NoError(t, db.Where("instance_id = ? AND snapshot_type = ?", instance.Id, model.ManagedInstanceSnapshotTypeInventory).First(&snapshot).Error)
	require.Equal(t, view.ETag, snapshot.ETag)
	require.Contains(t, snapshot.Payload, `"id":1`)
	require.Contains(t, snapshot.Payload, `"id":101`)
}

func TestCollectSummaryMarksUnavailableMetricsInsteadOfZero(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writeProbeJSON(response, `{"code":0,"data":[{"id":9,"name":"upstream-a","status":"active"},{"id":10,"name":"upstream-b","status":"expired"}]}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "admin-secret"})

	view, err := CollectSummary(context.Background(), instance.Id, TimeWindow{Start: 100, End: 200})
	require.NoError(t, err)
	summary, ok := view.Data.(*SummaryResult)
	require.True(t, ok)
	require.Equal(t, 2, summary.Resources[0].Total)
	require.Equal(t, 1, *summary.Resources[0].Enabled)
	require.Equal(t, 1, *summary.Resources[0].Unhealthy)
	require.Nil(t, summary.Requests.Value)
	require.Equal(t, model.ManagedInstanceCollectionUnsupported, summary.Requests.CollectionStatus)

	encoded, err := json.Marshal(summary)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"value":null`)
}

func TestCollectInventoryPersistsFailureWithoutSyntheticCounts(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "bad-secret"})

	view, err := CollectInventory(context.Background(), instance.Id, "channel", "")
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceCollectionFailed, view.CollectionStatus)
	require.Equal(t, ProbeErrorAuthentication, view.ErrorCode)
	require.Nil(t, view.Data)

	var snapshot model.ManagedInstanceSnapshot
	require.NoError(t, db.Where("instance_id = ? AND snapshot_type = ?", instance.Id, model.ManagedInstanceSnapshotTypeInventory).First(&snapshot).Error)
	require.Equal(t, "null", snapshot.Payload)
	require.Equal(t, ProbeErrorAuthentication, snapshot.ErrorCode)
}

func TestProbeConnectionDoesNotPersistInstanceOrCredential(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/status":
			writeProbeJSON(response, `{"success":true,"data":{"version":"v2","system_name":"New API","start_time":10}}`)
		case "/api/status/test":
			require.Equal(t, "Bearer preflight-secret", request.Header.Get("Authorization"))
			writeProbeJSON(response, `{"success":true}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := ProbeConnection(context.Background(), CreateInput{
		Name: "preflight", Kind: model.ManagedInstanceKindNewAPI, BaseURL: server.URL,
		Environment: "development", ManagementMode: model.ManagedInstanceModeObserve, TLSVerify: true,
		Credential: &CredentialInput{AuthType: "bearer_pat", Secret: "preflight-secret"}, ActorID: 1,
	})
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "v2", result.Probe.Version)
	for _, stage := range result.Stages {
		require.Equal(t, "succeeded", stage.Status)
	}
	var instanceCount int64
	var credentialCount int64
	require.NoError(t, db.Model(&model.ManagedInstance{}).Count(&instanceCount).Error)
	require.NoError(t, db.Model(&model.ManagedInstanceCredential{}).Count(&credentialCount).Error)
	require.Zero(t, instanceCount)
	require.Zero(t, credentialCount)
}

func TestPreflightStagesIncludeAuthenticationCapabilityAndTLSClassification(t *testing.T) {
	authStages := failedConnectionStages(&ProbeError{Code: ProbeErrorAuthentication})
	require.Equal(t, "authentication", authStages[4].Name)
	require.Equal(t, "failed", authStages[4].Status)
	require.Equal(t, "not_run", authStages[5].Status)

	tlsStages := failedConnectionStages(x509.UnknownAuthorityError{})
	require.Equal(t, "tls", tlsStages[2].Name)
	require.Equal(t, "failed", tlsStages[2].Status)
	require.Equal(t, "not_run", tlsStages[3].Status)

	successStages := succeededConnectionStages()
	require.Len(t, successStages, 6)
	for _, stage := range successStages {
		require.Equal(t, "succeeded", stage.Status)
	}
}
