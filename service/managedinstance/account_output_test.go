package managedinstance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestCollectAccountOutputFiltersNewAPIChannelsAndAggregatesUsage(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/channel/":
			writeProbeJSON(response, `{"success":true,"data":{"items":[{"id":1,"name":"new","created_at":150,"status":1},{"id":2,"name":"old","created_at":50,"status":1}],"total":2}}`)
		case "/api/log/":
			require.Equal(t, "1", request.URL.Query().Get("channel"))
			require.Equal(t, "100", request.URL.Query().Get("start_timestamp"))
			require.Equal(t, "200", request.URL.Query().Get("end_timestamp"))
			if request.URL.Query().Get("p") != "1" {
				writeProbeJSON(response, `{"success":true,"data":{"items":[],"total":2,"page":2,"page_size":100}}`)
				return
			}
			writeProbeJSON(response, `{"success":true,"data":{"items":[{"prompt_tokens":10,"completion_tokens":5,"quota":20},{"prompt_tokens":3,"completion_tokens":2,"quota":10}],"total":2,"page":1,"page_size":100}}`)
		case "/api/status":
			writeProbeJSON(response, `{"success":true,"data":{"quota_per_unit":10}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "secret", UserID: "1"})

	view, err := CollectAccountOutput(context.Background(), instance.Id, TimeWindow{Start: 100, End: 200})
	require.NoError(t, err)
	result := view.Data.(*AccountOutputResult)
	require.Equal(t, 1, result.AddedAccounts)
	require.Equal(t, 1, result.CollectedAccounts)
	require.Equal(t, 2.0, result.TotalRequests)
	require.Equal(t, 20.0, result.TotalTokens)
	require.Equal(t, 3.0, result.TotalAmount)
	require.Equal(t, "USD", result.Currency)
	require.Equal(t, "new", result.Items[0].Account.Name)
}

func TestCollectAccountOutputUsesSub2AccountFilter(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/admin/accounts":
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":7,"name":"new-account","created_at":150,"status":"active"}],"total":1,"page":1,"page_size":100,"pages":1}}`)
		case "/api/v1/admin/usage":
			require.Equal(t, "7", request.URL.Query().Get("account_id"))
			if request.URL.Query().Get("page") != "1" {
				writeProbeJSON(response, `{"code":0,"data":{"items":[],"total":1,"page":2,"page_size":100,"pages":1}}`)
				return
			}
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"input_tokens":100,"output_tokens":50,"cache_read_tokens":25,"cache_creation_tokens":5,"actual_cost":1.25}],"total":1,"page":1,"page_size":100,"pages":1}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "secret"})

	view, err := CollectAccountOutput(context.Background(), instance.Id, TimeWindow{Start: 100, End: 200})
	require.NoError(t, err)
	result := view.Data.(*AccountOutputResult)
	require.Equal(t, 1, result.AddedAccounts)
	require.Equal(t, 1.0, result.TotalRequests)
	require.Equal(t, 180.0, result.TotalTokens)
	require.Equal(t, 1.25, result.TotalAmount)
}

func TestCollectAccountOutputMarksConductorAccountUsageUnsupported(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/v1/accounts", request.URL.Path)
		writeProbeJSON(response, `{"code":200,"data":{"accounts":[{"account_id":"9","label":"conductor-account","created_at":150,"available":true}],"total":1}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindConductor, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	view, err := CollectAccountOutput(context.Background(), instance.Id, TimeWindow{Start: 100, End: 200})
	require.NoError(t, err)
	result := view.Data.(*AccountOutputResult)
	require.Equal(t, 1, result.AddedAccounts)
	require.Equal(t, model.ManagedInstanceCollectionUnsupported, result.Items[0].CollectionStatus)
}

func TestCollectAccountOutputUsesRegularNewAPIAccountWithoutAdminFilter(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/user/self":
			writeProbeJSON(response, `{"success":true,"data":{"id":8,"username":"regular","role":1,"status":1,"created_at":150}}`)
		case "/api/data/self":
			require.Empty(t, request.URL.Query().Get("channel"))
			writeProbeJSON(response, `{"success":true,"data":[{"count":4,"token_used":25,"quota":20}]}`)
		case "/api/status":
			writeProbeJSON(response, `{"success":true,"data":{"quota_per_unit":10}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{
		AuthType: "bearer_pat", AccessScope: model.ManagedInstanceAccessUser, Secret: "secret", UserID: "8",
	})

	view, err := CollectAccountOutput(context.Background(), instance.Id, TimeWindow{Start: 100, End: 200})
	require.NoError(t, err)
	result := view.Data.(*AccountOutputResult)
	require.Equal(t, 1, result.AddedAccounts)
	require.Equal(t, 1, result.CollectedAccounts)
	require.Equal(t, 4.0, result.TotalRequests)
	require.Equal(t, 25.0, result.TotalTokens)
	require.Equal(t, 2.0, result.TotalAmount)
}

func TestCollectAccountOutputReusesRecentInventorySnapshot(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/admin/accounts":
			t.Fatal("recent inventory snapshot must prevent another account crawl")
		case "/api/v1/admin/usage":
			require.Equal(t, "7", request.URL.Query().Get("account_id"))
			if request.URL.Query().Get("page") != "1" {
				writeProbeJSON(response, `{"code":0,"data":{"items":[],"total":1,"page":2,"page_size":100,"pages":1}}`)
				return
			}
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"input_tokens":10,"output_tokens":5,"actual_cost":0.25}],"total":1,"page":1,"page_size":100,"pages":1}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "secret"})
	page := &InventoryPage{ResourceKind: "account", Total: 1, Items: []InventoryItem{{ID: 7, Name: "cached", CreatedAt: 150}}}
	_, err := persistObservation(instance.Id, model.ManagedInstanceSnapshotTypeInventory, "account", common.GetTimestamp(), page, nil)
	require.NoError(t, err)

	view, err := CollectAccountOutput(context.Background(), instance.Id, TimeWindow{Start: 100, End: 200})
	require.NoError(t, err)
	result := view.Data.(*AccountOutputResult)
	require.Equal(t, 1, result.AddedAccounts)
	require.Equal(t, 15.0, result.TotalTokens)
}
