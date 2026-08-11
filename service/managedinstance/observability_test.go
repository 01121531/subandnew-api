package managedinstance

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestCollectInventoryAggregatesSub2APIPages(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	requestedPages := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/v1/admin/accounts", request.URL.Path)
		require.Equal(t, "100", request.URL.Query().Get("page_size"))
		page := request.URL.Query().Get("page")
		requestedPages = append(requestedPages, page)
		switch page {
		case "1":
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":1,"name":"first"}],"total":3,"page":1,"page_size":1,"pages":3}}`)
		case "2":
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":2,"name":"second"}],"total":3,"page":2,"page_size":1,"pages":3}}`)
		case "3":
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":3,"name":"third"}],"total":3,"page":3,"page_size":1,"pages":3}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "admin-secret"})

	view, err := CollectInventory(context.Background(), instance.Id, "account", "")
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceCollectionSucceeded, view.CollectionStatus)
	require.Equal(t, []string{"1", "2", "3"}, requestedPages)
	page := view.Data.(*InventoryPage)
	require.Equal(t, 3, page.Total)
	require.Len(t, page.Items, 3)
	require.Empty(t, page.NextCursor)
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

func TestCollectSummaryAggregatesSub2APIUsageData(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	start := time.Date(2026, time.August, 8, 10, 0, 0, 0, time.UTC).Unix()
	end := start + 24*60*60
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "admin-secret", request.Header.Get("x-api-key"))
		switch request.URL.Path {
		case "/api/v1/admin/accounts":
			writeProbeJSON(response, `{"code":0,"data":[{"id":9,"name":"upstream-a","status":"active"}]}`)
		case "/api/v1/admin/dashboard/snapshot-v2":
			require.Equal(t, "2026-08-08", request.URL.Query().Get("start_date"))
			require.Equal(t, "2026-08-09", request.URL.Query().Get("end_date"))
			require.Equal(t, "UTC", request.URL.Query().Get("timezone"))
			require.Equal(t, "day", request.URL.Query().Get("granularity"))
			require.Equal(t, "false", request.URL.Query().Get("include_stats"))
			require.Equal(t, "true", request.URL.Query().Get("include_trend"))
			writeProbeJSON(response, `{"code":0,"data":{"trend":[{"date":"2026-08-08","requests":7,"total_tokens":1250,"actual_cost":1.25},{"date":"2026-08-09","requests":5,"total_tokens":750,"actual_cost":0.75}]}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "admin-secret"})

	view, err := CollectSummary(context.Background(), instance.Id, TimeWindow{Start: start, End: end})
	require.NoError(t, err)
	summary := view.Data.(*SummaryResult)
	require.Equal(t, 12.0, *summary.Requests.Value)
	require.Equal(t, 2000.0, *summary.Tokens.Value)
	require.Equal(t, 2.0, *summary.Cost.Value)
	require.Equal(t, "usd", summary.Cost.Unit)
	require.Equal(t, model.ManagedInstanceCollectionSucceeded, summary.Requests.CollectionStatus)
	require.Len(t, summary.Trend, 2)
	require.Equal(t, "2026-08-08", summary.Trend[0].Date)
	require.Equal(t, 7.0, summary.Trend[0].Requests)
	require.Equal(t, "2026-08-09", summary.Trend[1].Date)
}

func TestCollectSummaryUsesSub2APIRegularAccountData(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	start := time.Date(2026, time.August, 8, 10, 0, 0, 0, time.UTC).Unix()
	end := start + 24*60*60
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/auth/login":
			writeProbeJSON(response, `{"code":0,"data":{"access_token":"user-token"}}`)
		case "/api/v1/user/profile":
			require.Equal(t, "Bearer user-token", request.Header.Get("Authorization"))
			writeProbeJSON(response, `{"code":0,"data":{"id":42,"email":"user@example.com","username":"User","role":"user","status":"active"}}`)
		case "/api/v1/usage/dashboard/snapshot-v2":
			require.Equal(t, "true", request.URL.Query().Get("include_trend"))
			require.Equal(t, "day", request.URL.Query().Get("granularity"))
			writeProbeJSON(response, `{"code":0,"data":{"trend":[{"date":"2026-08-08","requests":3,"total_tokens":500,"actual_cost":0.25}]}}`)
		case "/api/v1/admin/accounts", "/api/v1/admin/dashboard/snapshot-v2":
			t.Fatal("regular account collection must not call an administrator endpoint")
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{
		AuthType: "account_password", AccessScope: model.ManagedInstanceAccessUser,
		Secret: "password", UserID: "user@example.com",
	})

	inventoryView, err := CollectInventory(context.Background(), instance.Id, "auto", "")
	require.NoError(t, err)
	inventory := inventoryView.Data.(*InventoryPage)
	require.Equal(t, "user", inventory.ResourceKind)
	require.Equal(t, 1, inventory.Total)
	require.Equal(t, "User", inventory.Items[0].Name)

	summaryView, err := CollectSummary(context.Background(), instance.Id, TimeWindow{Start: start, End: end})
	require.NoError(t, err)
	summary := summaryView.Data.(*SummaryResult)
	require.Equal(t, "user", summary.Resources[0].ResourceKind)
	require.Equal(t, 3.0, *summary.Requests.Value)
	require.Equal(t, 500.0, *summary.Tokens.Value)
	require.Equal(t, 0.25, *summary.Cost.Value)
	require.Len(t, summary.Trend, 2)
	require.Equal(t, 3.0, summary.Trend[0].Requests)
	require.Zero(t, summary.Trend[1].Requests)
}

func TestCollectSummaryAggregatesNewAPIUsageData(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer metrics-secret", request.Header.Get("Authorization"))
		require.Equal(t, "1", request.Header.Get("New-Api-User"))
		switch request.URL.Path {
		case "/api/channel/":
			writeProbeJSON(response, `{"success":true,"data":{"items":[{"id":1,"name":"primary","status":1}],"total":1}}`)
		case "/api/data/":
			require.Equal(t, "100", request.URL.Query().Get("start_timestamp"))
			require.Equal(t, "200", request.URL.Query().Get("end_timestamp"))
			writeProbeJSON(response, `{"success":true,"data":[{"created_at":120,"token_used":1200,"count":8,"quota":45.5},{"created_at":180,"token_used":800,"count":5,"quota":24.5}]}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "metrics-secret", UserID: "1"})

	view, err := CollectSummary(context.Background(), instance.Id, TimeWindow{Start: 100, End: 200})
	require.NoError(t, err)
	summary := view.Data.(*SummaryResult)
	require.Equal(t, 13.0, *summary.Requests.Value)
	require.Equal(t, 2000.0, *summary.Tokens.Value)
	require.Equal(t, 70.0, *summary.Cost.Value)
	require.Equal(t, "quota", summary.Cost.Unit)
	require.Equal(t, model.ManagedInstanceCollectionSucceeded, summary.Requests.CollectionStatus)
	require.Len(t, summary.Trend, 1)
	require.Equal(t, "1970-01-01", summary.Trend[0].Date)
	require.Equal(t, 13.0, summary.Trend[0].Requests)
	require.Equal(t, 2000.0, summary.Trend[0].Tokens)
	require.Equal(t, 70.0, summary.Trend[0].Cost)
}

func TestConductorInventoryAndSummary(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/auth/login":
			writeProbeJSON(response, `{"code":200,"data":{"token":"conductor-token"}}`)
		case "/api/v1/accounts":
			require.Equal(t, "Bearer conductor-token", request.Header.Get("Authorization"))
			require.Equal(t, "0", request.URL.Query().Get("offset"))
			require.Equal(t, "100", request.URL.Query().Get("limit"))
			writeProbeJSON(response, `{"code":200,"data":{"accounts":[{"account_id":"101","email":"one@example.com","label":"Primary","auth_type":"oauth","health":"Healthy","available":true},{"account_id":"102","email":"two@example.com","auth_type":"oauth","status":"Paused","available":false}],"total":2}}`)
		case "/api/v1/system/health":
			writeProbeJSON(response, `{"code":200,"data":{"status":"ok","accounts_total":2,"accounts_available":1,"accounts_paused":1,"accounts_rejected":0}}`)
		case "/api/v1/system/stats":
			writeProbeJSON(response, `{"code":200,"data":{"usage":{"recorded":7}}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindConductor, CredentialInput{
		AuthType: "account_password", Secret: "password", UserID: "Cli@mini",
	})

	inventoryView, err := CollectInventory(context.Background(), instance.Id, "auto", "")
	require.NoError(t, err)
	inventory := inventoryView.Data.(*InventoryPage)
	require.Equal(t, 2, inventory.Total)
	require.Equal(t, "Primary", inventory.Items[0].Name)
	require.True(t, *inventory.Items[0].Enabled)

	summaryView, err := CollectSummary(context.Background(), instance.Id, TimeWindow{Start: 100, End: 200})
	require.NoError(t, err)
	summary := summaryView.Data.(*SummaryResult)
	require.Equal(t, 2, summary.Resources[0].Total)
	require.Equal(t, 1, *summary.Resources[0].Enabled)
	require.Equal(t, 1, *summary.Resources[0].Unhealthy)
	require.Equal(t, 7.0, *summary.Requests.Value)
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
	require.Equal(t, "tls_verification_failed", managedInstanceObservationErrorCode(x509.UnknownAuthorityError{}))

	successStages := succeededConnectionStages()
	require.Len(t, successStages, 6)
	for _, stage := range successStages {
		require.Equal(t, "succeeded", stage.Status)
	}
}
