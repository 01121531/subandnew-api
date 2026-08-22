package managedinstance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestProbeGenericDetectsClaudeGateway(t *testing.T) {
	newManagedInstanceTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/status", "/health", "/api/v1/auth/login", "/api/v1/system/health":
			http.NotFound(response, request)
		case "/api/health":
			writeProbeJSON(response, `{"status":"ok","service":"2coding-gateway-api"}`)
		case "/api/auth/admin-login":
			var input map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&input))
			require.Equal(t, "admin", input["identifier"])
			require.Equal(t, "admin", input["email"])
			require.Equal(t, "password", input["password"])
			http.SetCookie(response, &http.Cookie{Name: "refresh_token", Value: "refresh", Path: "/"})
			writeProbeJSON(response, `{"accessToken":"access"}`)
		case "/api/auth/me":
			require.Equal(t, "Bearer access", request.Header.Get("Authorization"))
			writeProbeJSON(response, `{"user":{"role":"admin"}}`)
		case "/api/admin/oauth-accounts/today-summary":
			require.Equal(t, "Bearer access", request.Header.Get("Authorization"))
			writeProbeJSON(response, `{"total_cost":1.25,"total_cost_7d":5.5,"request_count":8}`)
		case "/api/admin/system/info":
			writeProbeJSON(response, `{"version":"0.1.0"}`)
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
	require.Equal(t, model.ManagedInstanceKindClaudeGateway, result.Kind)
	require.Equal(t, "Claude Gateway", result.SystemName)
	require.Equal(t, "0.1.0", result.Version)
	require.Contains(t, result.Capabilities, "accounts.list")

	stored, err := Get(instance.Id)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindClaudeGateway, stored.Kind)
}

func TestClaudeGatewayInventoryMapsAccountMetrics(t *testing.T) {
	newManagedInstanceTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/admin/oauth-accounts":
			require.Equal(t, "Bearer secret", request.Header.Get("Authorization"))
			writeProbeJSON(response, `{"accounts":[{"id":"8faa3804-86ab-4f4c-a090-e5111a406c74","name":"primary","account_type":"max","status":"active","health_status":"healthy","provider":"anthropic","group_name":"default","created_at":"2026-08-21T14:48:27.674Z","last_used_at":"1787414527000","total_requests":"42","total_tokens":1234,"total_cost":"9.875","stats":{"rpm":17,"concurrent":3,"active_sessions":2}}]}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindClaudeGateway, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	view, err := CollectInventory(context.Background(), instance.Id, "auto", "")
	require.NoError(t, err)
	page, ok := view.Data.(*InventoryPage)
	require.True(t, ok)
	require.Equal(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	item := page.Items[0]
	require.Positive(t, item.ID)
	require.Equal(t, "primary", item.Name)
	require.Equal(t, "max", item.Type)
	require.Equal(t, "anthropic", item.Platform)
	require.Equal(t, "default", item.Group)
	require.Equal(t, int64(1787323707), item.CreatedAt)
	require.Equal(t, int64(1787414527), item.LastActivityAt)
	require.True(t, *item.Enabled)
	require.Equal(t, 42.0, *item.Requests)
	require.Equal(t, 1234.0, *item.Tokens)
	require.Equal(t, 9.875, *item.Cost)
	require.Equal(t, 17, *item.RPM)
	require.Equal(t, 2, *item.ActiveSessions)
}

func TestRefreshClaudeGatewayRealtimeAggregatesAccounts(t *testing.T) {
	newManagedInstanceTestDB(t)
	resetNewAPIRealtimeCacheForTest()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/admin/oauth-accounts":
			writeProbeJSON(response, `{"accounts":[{"id":"one","name":"one","status":"active","health_status":"healthy","stats":{"rpm":12,"concurrent":2,"active_sessions":3}},{"id":"two","name":"two","status":"disabled","health_status":"failed","stats":{"rpm":5,"concurrent":1,"active_sessions":1}}]}`)
		case "/api/admin/oauth-accounts/today-summary":
			writeProbeJSON(response, `{"total_cost":12.34567891,"total_cost_7d":80,"request_count":100}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindClaudeGateway, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	state, err := RefreshClaudeGatewayRealtime(context.Background(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, 17.0, *state.RPM.Value)
	require.Equal(t, 3.0, *state.ConcurrencyUsed.Value)
	require.Equal(t, 12.34567891, *state.TodayCost.Value)
	require.Equal(t, 2, state.AccountsTotal)
	require.Equal(t, 1, state.AccountsAvailable)
	require.Equal(t, 4, state.ActiveSessions)
	require.Len(t, state.Accounts, 2)
}
