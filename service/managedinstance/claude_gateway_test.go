package managedinstance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestProbeGenericDetectsClaudeGateway(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
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
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	vendorRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/admin/oauth-accounts":
			require.Equal(t, "Bearer secret", request.Header.Get("Authorization"))
			writeProbeJSON(response, `{"accounts":[{"id":"8faa3804-86ab-4f4c-a090-e5111a406c74","name":"primary","owner_user_id":"vendor-1","account_type":"max","status":"active","health_status":"healthy","provider":"anthropic","group_name":"default","created_at":"2026-08-21T14:48:27.674Z","last_used_at":"1787414527000","total_requests":"42","total_tokens":1234,"total_cost":"9.875","stats":{"rpm":17,"concurrent":3,"active_sessions":2}}]}`)
		case "/api/admin/vendors":
			vendorRequests++
			require.Equal(t, "true", request.URL.Query().Get("include_usage"))
			require.Equal(t, "Bearer secret", request.Header.Get("Authorization"))
			writeProbeJSON(response, `{"items":[{"id":"vendor-1","username":"供应商 A","email":"vendor@example.com","status":"active"}]}`)
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
	require.Equal(t, "8faa3804-86ab-4f4c-a090-e5111a406c74", item.IDText)
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
	require.Equal(t, "vendor-1", item.VendorID)
	require.Equal(t, "供应商 A", item.VendorName)
	require.Equal(t, "vendor@example.com", item.VendorEmail)
	require.Equal(t, model.ManagedInstanceCollectionSucceeded, page.VendorCollectionStatus)
	require.False(t, page.VendorStale)
	require.Positive(t, page.VendorObservedAt)
	require.Equal(t, 1, vendorRequests)
}

func TestClaudeGatewayInventoryBulkRequestAndNumericVendorIDs(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	vendorRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/admin/oauth-accounts":
			time.Sleep(1100 * time.Millisecond)
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"accounts":[`))
			for index := range 5000 {
				if index > 0 {
					_, _ = response.Write([]byte(","))
				}
				_, _ = fmt.Fprintf(response, `{"id":"account-%d","name":"account-%d","owner_user_id":9007199254740993,"status":"active"}`, index, index)
			}
			_, _ = response.Write([]byte(`]}`))
		case "/api/admin/vendors":
			vendorRequests++
			writeProbeJSON(response, `{"data":{"vendors":[{"id":9007199254740993,"username":"large-id-vendor","email":"xw@qq.com"}]}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindClaudeGateway, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})
	require.NoError(t, model.DB.Model(&model.ManagedInstance{}).Where("id = ?", instance.Id).Update("request_timeout_seconds", 1).Error)

	view, err := CollectInventory(context.Background(), instance.Id, "auto", "")
	require.NoError(t, err)
	page := view.Data.(*InventoryPage)
	require.Len(t, page.Items, 5000)
	require.Equal(t, "9007199254740993", page.Items[0].VendorID)
	require.Equal(t, "large-id-vendor", page.Items[0].VendorName)
	require.Equal(t, "xw@qq.com", page.Items[0].VendorEmail)
	require.Equal(t, 1, vendorRequests)
}

func TestDecodeClaudeGatewayVendorsAcceptsKnownEnvelopes(t *testing.T) {
	for name, body := range map[string]string{
		"array":        `[{"id":"vendor-1"}]`,
		"items":        `{"items":[{"id":"vendor-1"}]}`,
		"vendors":      `{"vendors":[{"id":"vendor-1"}]}`,
		"data items":   `{"data":{"items":[{"id":"vendor-1"}]}}`,
		"data vendors": `{"data":{"vendors":[{"id":"vendor-1"}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			vendors, err := decodeClaudeGatewayVendors([]byte(body))
			require.NoError(t, err)
			require.Len(t, vendors, 1)
			require.Equal(t, "vendor-1", vendors[0].ID)
		})
	}
	_, err := decodeClaudeGatewayVendors([]byte(`{"data":{"unknown":[]}}`))
	require.Error(t, err)
}

func TestClaudeGatewayAccountVendorFallbackLabels(t *testing.T) {
	platformOwned := claudeGatewayAccountItem(claudeGatewayAccount{ID: "platform", Name: "platform"}, nil)
	require.Empty(t, platformOwned.VendorID)
	require.Equal(t, "平台自有", platformOwned.VendorName)
	require.Empty(t, platformOwned.VendorEmail)

	unknown := claudeGatewayAccountItem(claudeGatewayAccount{ID: "unknown", Name: "unknown", OwnerUserID: "missing"}, nil)
	require.Equal(t, "missing", unknown.VendorID)
	require.Equal(t, "未知供应商", unknown.VendorName)
	require.Empty(t, unknown.VendorEmail)
	require.Equal(t, 1, claudeGatewayUnmatchedVendorCount([]InventoryItem{platformOwned, unknown}))
}

func TestClaudeGatewayInventoryKeepsPreviousVendorsWhenCollectionFails(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ManagedAccountSnapshot{}))
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/admin/oauth-accounts":
			writeProbeJSON(response, `{"accounts":[{"id":"account-1","name":"primary","owner_user_id":"vendor-1","status":"active","health_status":"healthy"}]}`)
		case "/api/admin/vendors":
			http.Error(response, "unavailable", http.StatusBadGateway)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindClaudeGateway, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})
	previousObservedAt := time.Now().Add(-5 * time.Minute).Unix()
	payload, err := json.Marshal(InventoryPage{ResourceKind: "account", VendorObservedAt: previousObservedAt, Items: []InventoryItem{{
		ID: 1, IDText: "account-1", Name: "primary", VendorID: "vendor-1", VendorName: "旧供应商", VendorEmail: "old@example.com",
	}}})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ManagedAccountSnapshot{
		InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindInventory, RangeKey: "inventory",
		SchemaVersion: 3, ObservedAt: previousObservedAt, Payload: string(payload),
		LastAttemptAt: previousObservedAt, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)

	view, err := CollectInventory(context.Background(), instance.Id, "auto", "")
	require.NoError(t, err)
	page := view.Data.(*InventoryPage)
	require.Len(t, page.Items, 1)
	require.Equal(t, "旧供应商", page.Items[0].VendorName)
	require.Equal(t, "old@example.com", page.Items[0].VendorEmail)
	require.Equal(t, model.ManagedInstanceCollectionFailed, page.VendorCollectionStatus)
	require.True(t, page.VendorStale)
	require.NotEmpty(t, page.VendorErrorCode)
	require.Equal(t, previousObservedAt, page.VendorObservedAt)
}

func TestClaudeGatewayInventoryUsesHealthAndUsageWindows(t *testing.T) {
	var account claudeGatewayAccount
	require.NoError(t, json.Unmarshal([]byte(`{
		"id":"windowed",
		"name":"windowed",
		"status":"active",
		"health_status":"cooldown",
		"failure_kind":"rate_limit",
		"recovery_state":"cooldown",
		"created_at":"2026-08-25T08:00:00Z",
		"last_used_at":"",
		"disabled_at":"2026-08-25T09:30:00Z",
		"expires_at":"1787721160537",
		"total_requests":"999",
		"total_tokens":"9999",
		"total_cost":"99.9999",
		"req_24h":120,
		"ok_24h":100,
		"limited_24h":15,
		"usage_windows":{"req_30d":300,"tokens_30d":4000,"cost_30d":12.345678},
		"stats":{"cooldown":false,"cooldown_remaining_seconds":0}
	}`), &account))

	item := claudeGatewayAccountItem(account, nil)
	require.NotNil(t, item.Enabled)
	require.True(t, *item.Enabled)
	require.True(t, item.RateLimited)
	require.Equal(t, int64(1787650200), item.DisabledAt)
	require.Equal(t, int64(1787721160), item.ExpiresAt)
	require.Equal(t, 30, item.UsageWindowDays)
	require.Equal(t, 300.0, *item.Requests)
	require.Equal(t, 4000.0, *item.Tokens)
	require.Equal(t, 12.345678, *item.Cost)
	require.Equal(t, 120.0, *item.Requests24H)
	require.Equal(t, 100.0, *item.SuccessfulRequests24H)
	require.Equal(t, 15.0, *item.LimitedRequests24H)
}

func TestClaudeGatewayAccountCosts(t *testing.T) {
	tests := []struct {
		name       string
		payload    string
		lifetime   *float64
		today      *float64
		historical *float64
	}{
		{name: "normal", payload: `{"id":"normal","total_cost":"100.25","stats":{"daily_cost":"7.125"}}`, lifetime: float64Pointer(100.25), today: float64Pointer(7.125), historical: float64Pointer(93.125)},
		{name: "zero", payload: `{"id":"zero","total_cost":0,"stats":{"daily_cost":0}}`, lifetime: float64Pointer(0), today: float64Pointer(0), historical: float64Pointer(0)},
		{name: "tiny inversion", payload: `{"id":"tiny","total_cost":1,"stats":{"daily_cost":1.000000005}}`, lifetime: float64Pointer(1), today: float64Pointer(1.000000005), historical: float64Pointer(0)},
		{name: "invalid inversion", payload: `{"id":"invalid","total_cost":1,"stats":{"daily_cost":2}}`, lifetime: float64Pointer(1), today: float64Pointer(2)},
		{name: "missing today", payload: `{"id":"missing","total_cost":1,"stats":{}}`, lifetime: float64Pointer(1)},
		{name: "top-level aliases", payload: `{"id":"aliases","lifetime_cost":"9.5","today_cost":"1.25"}`, lifetime: float64Pointer(9.5), today: float64Pointer(1.25), historical: float64Pointer(8.25)},
		{name: "nested aliases", payload: `{"id":"nested","stats":{"total_cost":"20","today_cost":"3"}}`, lifetime: float64Pointer(20), today: float64Pointer(3), historical: float64Pointer(17)},
		{name: "usage aliases", payload: `{"id":"usage","usage":{"total_cost":12,"daily_cost":2}}`, lifetime: float64Pointer(12), today: float64Pointer(2), historical: float64Pointer(10)},
		{name: "negative lifetime", payload: `{"id":"negative","total_cost":-1,"stats":{"daily_cost":0}}`, today: float64Pointer(0)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var account claudeGatewayAccount
			require.NoError(t, json.Unmarshal([]byte(test.payload), &account))
			item := claudeGatewayAccountItem(account, nil)
			require.Equal(t, test.lifetime, item.LifetimeCost)
			require.Equal(t, test.today, item.TodayCost)
			require.Equal(t, test.historical, item.CostExcludingToday)
		})
	}
}

func TestClaudeGatewayAccountAvailableMatchesGatewayDashboard(t *testing.T) {
	tests := []struct {
		name          string
		status        string
		healthStatus  string
		cooldown      bool
		lastError     string
		wantAvailable bool
	}{
		{name: "healthy active", status: "active", healthStatus: "healthy", wantAvailable: true},
		{name: "unknown health active", status: "active", healthStatus: "unknown", wantAvailable: true},
		{name: "failed health active", status: "active", healthStatus: "failed", wantAvailable: true},
		{name: "historical rate limit active", status: "active", healthStatus: "healthy", lastError: "upstream rate_limit", wantAvailable: true},
		{name: "cooldown active", status: "active", healthStatus: "cooldown", cooldown: true, wantAvailable: false},
		{name: "disabled", status: "disabled", healthStatus: "healthy", wantAvailable: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			account := claudeGatewayAccount{
				Status:       test.status,
				HealthStatus: test.healthStatus,
				LastError:    test.lastError,
			}
			account.Stats.Cooldown = test.cooldown
			require.Equal(t, test.wantAvailable, claudeGatewayAccountAvailable(account))
		})
	}
}

func TestClaudeGatewayAccountOutputAcceptsLargeInventoryResponse(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/admin/oauth-accounts":
			response.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(response).Encode(map[string]any{
				"accounts": []map[string]any{{
					"id": "large-account", "name": "large-account", "status": "active", "health_status": "healthy",
					"created_at":    "1970-01-01T00:02:30Z",
					"usage_windows": map[string]any{"req_30d": 123, "tokens_30d": 456, "cost_30d": 7.89},
				}},
				"padding": strings.Repeat("x", int(defaultConnectorMaxBodyBytes)),
			}))
		case "/api/admin/vendors":
			writeProbeJSON(response, `{"items":[]}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindClaudeGateway, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	view, err := CollectAccountOutput(context.Background(), instance.Id, TimeWindow{Start: 100, End: 200})
	require.NoError(t, err)
	result := view.Data.(*AccountOutputResult)
	require.Equal(t, 1, result.AddedAccounts)
	require.Equal(t, 1, result.CollectedAccounts)
	require.Equal(t, 123.0, result.TotalRequests)
	require.Equal(t, 456.0, result.TotalTokens)
	require.Equal(t, 7.89, result.TotalAmount)
}

func TestRefreshClaudeGatewayRealtimeAggregatesAccounts(t *testing.T) {
	newManagedInstanceTestDB(t)
	resetNewAPIRealtimeCacheForTest()
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	summaryRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/admin/oauth-accounts":
			writeProbeJSON(response, `{"accounts":[{"id":"one","name":"one","status":"active","health_status":"healthy","stats":{"rpm":12,"concurrent":2,"active_sessions":3}},{"id":"two","name":"two","status":"active","health_status":"cooldown","stats":{"rpm":5,"concurrent":1,"active_sessions":1,"cooldown":true}},{"id":"three","name":"three","status":"active","health_status":"unknown","stats":{"rpm":0,"cooldown":false}},{"id":"four","name":"four","status":"disabled","health_status":"failed","stats":{"rpm":0,"cooldown":false}}]}`)
		case "/api/admin/oauth-accounts/today-summary":
			summaryRequests++
			writeProbeJSON(response, `{"total_cost":12.34567891,"total_cost_7d":80,"total_cost_30d":320.5,"request_count":100}`)
		case "/api/admin/overview":
			require.Equal(t, "time", request.URL.Query().Get("slice"))
			require.Equal(t, "day", request.URL.Query().Get("granularity"))
			writeProbeJSON(response, `{"kpis":{"total":200,"successRate":0.975}}`)
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
	require.Nil(t, state.TodayCost.Value)
	require.Zero(t, summaryRequests)
	require.Equal(t, 0.975, *state.SuccessRate.Value)
	require.Equal(t, 200.0, state.SuccessRateSampleCount)
	require.Equal(t, 4, state.AccountsTotal)
	require.Equal(t, 2, state.AccountsAvailable)
	require.Equal(t, 4, state.ActiveSessions)
	require.Len(t, state.Accounts, 4)

	costState, err := RefreshClaudeGatewayCosts(context.Background(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, 1, summaryRequests)
	require.Equal(t, 12.34567891, *costState.TodayCost.Value)
	require.Equal(t, 80.0, *costState.Cost7D.Value)
	require.Equal(t, 320.5, *costState.Cost30D.Value)
	require.False(t, costState.TodayCostStale)
	require.False(t, costState.Cost7DStale)
	require.False(t, costState.Cost30DStale)
	require.Positive(t, costState.TodayCostObservedAt)
	require.Equal(t, costState.TodayCostObservedAt, costState.Cost7DObservedAt)
	require.Equal(t, costState.TodayCostObservedAt, costState.Cost30DObservedAt)

	state, err = RefreshClaudeGatewayRealtime(context.Background(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, 1, summaryRequests, "10-second realtime refresh must reuse cached costs")
	require.Equal(t, 12.34567891, *state.TodayCost.Value)
	require.Equal(t, 80.0, *state.Cost7D.Value)
	require.Equal(t, 320.5, *state.Cost30D.Value)
	var history model.ManagedRPMHistory
	require.NoError(t, model.DB.Where("instance_id = ?", instance.Id).Order("bucket_start desc").First(&history).Error)
	require.Equal(t, 1, history.TodayCostSampleCount)
	require.Equal(t, 1, history.Cost7DSampleCount)
	require.Equal(t, 1, history.Cost30DSampleCount)
}

func TestRefreshClaudeGatewayRealtimePreservesEachFailedCost(t *testing.T) {
	newManagedInstanceTestDB(t)
	resetNewAPIRealtimeCacheForTest()
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	summaryAttempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/admin/oauth-accounts":
			writeProbeJSON(response, `{"accounts":[{"id":"one","name":"one","status":"active","stats":{"rpm":1}}]}`)
		case "/api/admin/oauth-accounts/today-summary":
			summaryAttempt++
			switch summaryAttempt {
			case 1:
				writeProbeJSON(response, `{"total_cost":0,"total_cost_7d":7.5,"total_cost_30d":30.5}`)
			case 2:
				writeProbeJSON(response, `{"total_cost":2.5}`)
			default:
				http.Error(response, "temporary failure", http.StatusBadGateway)
			}
		case "/api/admin/overview":
			writeProbeJSON(response, `{"kpis":{"total":1,"successRate":1}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindClaudeGateway, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	first, err := RefreshClaudeGatewayCosts(context.Background(), instance.Id)
	require.NoError(t, err)
	require.Zero(t, *first.TodayCost.Value)
	require.Equal(t, 7.5, *first.Cost7D.Value)
	require.Equal(t, 30.5, *first.Cost30D.Value)

	partial, err := RefreshClaudeGatewayCosts(context.Background(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, 2.5, *partial.TodayCost.Value)
	require.False(t, partial.TodayCostStale)
	require.Equal(t, 7.5, *partial.Cost7D.Value)
	require.True(t, partial.Cost7DStale)
	require.Equal(t, 30.5, *partial.Cost30D.Value)
	require.True(t, partial.Cost30DStale)

	failed, err := RefreshClaudeGatewayCosts(context.Background(), instance.Id)
	require.Error(t, err)
	require.False(t, failed.Stale, "cost failures must not mark realtime metrics stale")
	require.Equal(t, 2.5, *failed.TodayCost.Value)
	require.True(t, failed.TodayCostStale)
	require.Equal(t, 7.5, *failed.Cost7D.Value)
	require.True(t, failed.Cost7DStale)
	require.Equal(t, 30.5, *failed.Cost30D.Value)
	require.True(t, failed.Cost30DStale)
}

func TestClaudeGatewaySummaryUsesExactCustomRange(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/admin/usage/keys", request.URL.Path)
		require.Equal(t, "custom", request.URL.Query().Get("range"))
		require.Equal(t, "2026-08-18", request.URL.Query().Get("from"))
		require.Equal(t, "2026-08-18", request.URL.Query().Get("to"))
		require.Equal(t, "1", request.URL.Query().Get("limit"))
		writeProbeJSON(response, `{"items":[],"summary":{"total_keys":5,"total_requests":123,"total_tokens":456,"total_cost":78.90123456},"total":5}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindClaudeGateway, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	start := time.Date(2026, 8, 18, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60)).Unix()
	end := start + 24*60*60 - 1
	view, err := CollectSummary(context.Background(), instance.Id, TimeWindow{Start: start, End: end, Timezone: "Asia/Shanghai"})
	require.NoError(t, err)
	summary := view.Data.(*SummaryResult)
	require.Equal(t, 123.0, *summary.Requests.Value)
	require.Equal(t, 456.0, *summary.Tokens.Value)
	require.Equal(t, 78.90123456, *summary.Cost.Value)
}

func TestClaudeGatewaySummaryDoesNotDuplicateOfficialCostRequest(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/admin/usage/keys":
			writeProbeJSON(response, `{"items":[],"summary":{"total_keys":5,"total_requests":123,"total_tokens":456,"total_cost":999},"total":5}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindClaudeGateway, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	localNow := time.Now().In(location)
	dayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	dayEnd := dayStart.AddDate(0, 0, 1).Add(-time.Second)
	for _, days := range []int{1, 7, 30} {
		view, collectErr := CollectSummary(context.Background(), instance.Id, TimeWindow{
			Start: dayStart.AddDate(0, 0, -(days - 1)).Unix(), End: dayEnd.Unix(), Timezone: location.String(),
		})
		require.NoError(t, collectErr)
		summary := view.Data.(*SummaryResult)
		require.Equal(t, 123.0, *summary.Requests.Value)
		require.Equal(t, 456.0, *summary.Tokens.Value)
		require.Equal(t, 999.0, *summary.Cost.Value)
	}
}
