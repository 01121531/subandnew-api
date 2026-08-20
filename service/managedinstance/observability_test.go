package managedinstance

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
		writeProbeJSON(response, `{"success":true,"data":{"items":[{"id":7,"name":"primary","type":1,"group":"default","status":1,"created_time":1723100000,"test_time":1723100300,"response_time":245,"balance":12.5,"used_quota":4096,"key":"must-not-leak","password":"must-not-leak"}],"total":1}}`)
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
	require.Equal(t, "1", page.Items[0].Type)
	require.Equal(t, int64(1723100000), page.Items[0].CreatedAt)
	require.Equal(t, int64(1723100300), page.Items[0].LastActivityAt)
	require.Equal(t, int64(245), *page.Items[0].ResponseTimeMS)
	require.Equal(t, 12.5, *page.Items[0].Balance)
	require.Equal(t, 4096.0, *page.Items[0].Cost)
	require.Equal(t, "quota", page.Items[0].CostUnit)
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
	requestedPages := make([]string, 0, 9)
	var requestedPagesMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/v1/admin/accounts/") && strings.HasSuffix(request.URL.Path, "/stats") {
			require.Equal(t, http.MethodGet, request.Method)
			days, err := strconv.Atoi(request.URL.Query().Get("days"))
			require.NoError(t, err)
			require.GreaterOrEqual(t, days, 1)
			require.LessOrEqual(t, days, 90)
			parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
			id, err := strconv.ParseInt(parts[len(parts)-2], 10, 64)
			require.NoError(t, err)
			writeProbeJSON(response, fmt.Sprintf(`{"code":0,"data":{"summary":{"total_requests":%d,"total_tokens":%d,"total_cost":%.2f}}}`, id*10, id*1000, float64(id)/10))
			return
		}
		require.Equal(t, "/api/v1/admin/accounts", request.URL.Path)
		require.Equal(t, "100", request.URL.Query().Get("page_size"))
		page := request.URL.Query().Get("page")
		requestedPagesMu.Lock()
		requestedPages = append(requestedPages, page)
		requestedPagesMu.Unlock()
		switch page {
		case "1":
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":1,"name":"first","platform":"claude","type":"oauth","status":"active","schedulable":true,"created_at":"2026-08-01T10:00:00Z","last_used_at":"2026-08-10T12:30:00Z"}],"total":3,"page":1,"page_size":1,"pages":3}}`)
		case "2":
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":2,"name":"second","platform":"openai","type":"apikey","status":"active","schedulable":false,"created_at":"2026-08-02T10:00:00Z","error_message":"rate limited"}],"total":3,"page":2,"page_size":1,"pages":3}}`)
		case "3":
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":3,"name":"third","created_at":"2026-08-03T10:00:00Z"}],"total":3,"page":3,"page_size":1,"pages":3}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "admin-secret"})

	view, err := CollectInventory(context.Background(), instance.Id, "account", "")
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceCollectionSucceeded, view.CollectionStatus)
	require.ElementsMatch(t, []string{"1", "2", "3", "4"}, requestedPages)
	page := view.Data.(*InventoryPage)
	require.Equal(t, 3, page.Total)
	require.Len(t, page.Items, 3)
	require.Empty(t, page.NextCursor)
	require.Equal(t, "claude", page.Items[0].Platform)
	require.Equal(t, "oauth", page.Items[0].Type)
	require.Equal(t, time.Date(2026, time.August, 1, 10, 0, 0, 0, time.UTC).Unix(), page.Items[0].CreatedAt)
	require.Equal(t, time.Date(2026, time.August, 10, 12, 30, 0, 0, time.UTC).Unix(), page.Items[0].LastActivityAt)
	require.True(t, *page.Items[0].Enabled)
	require.Equal(t, 10.0, *page.Items[0].Requests)
	require.Equal(t, 1000.0, *page.Items[0].Tokens)
	require.Equal(t, 0.1, *page.Items[0].Cost)
	require.Equal(t, "usd", page.Items[0].CostUnit)
	require.False(t, *page.Items[1].Enabled)
	require.Equal(t, "rate limited", page.Items[1].ErrorMessage)
	require.True(t, page.Items[1].RateLimited)
}

func TestNormalizeInventoryItemUsesActiveSub2RateLimitWindows(t *testing.T) {
	future := time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	past := time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
	for _, field := range []string{"rate_limit_reset_at", "overload_until", "temp_unschedulable_until"} {
		t.Run(field, func(t *testing.T) {
			raw := json.RawMessage(fmt.Sprintf(`{"id":1,"status":"active","schedulable":true,%q:%q}`, field, future))
			item, ok := normalizeInventoryItem(raw)
			require.True(t, ok)
			require.NotNil(t, item.Enabled)
			require.True(t, *item.Enabled)
			require.True(t, item.RateLimited)
		})
	}

	raw := json.RawMessage(fmt.Sprintf(`{"id":1,"status":"active","schedulable":true,"rate_limit_reset_at":%q}`, past))
	item, ok := normalizeInventoryItem(raw)
	require.True(t, ok)
	require.False(t, item.RateLimited)
}

func TestCollectInventoryAggregatesConductorPagesConcurrentlyDespiteLowReportedTotal(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	requestedOffsets := make([]string, 0, 9)
	var requestedOffsetsMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/ws-clients" {
			writeProbeJSON(response, `{"code":200,"data":[]}`)
			return
		}
		require.Equal(t, "/api/v1/accounts", request.URL.Path)
		offset := request.URL.Query().Get("offset")
		requestedOffsetsMu.Lock()
		requestedOffsets = append(requestedOffsets, offset)
		requestedOffsetsMu.Unlock()
		switch offset {
		case "0":
			writeProbeJSON(response, `{"code":200,"data":{"accounts":[{"account_id":"1","label":"first","available":true}],"total":1}}`)
		case "1":
			writeProbeJSON(response, `{"code":200,"data":{"accounts":[{"account_id":"2","label":"second","available":true}],"total":1}}`)
		case "2":
			writeProbeJSON(response, `{"code":200,"data":{"accounts":[{"account_id":"2","label":"duplicate","available":true},{"account_id":"3","label":"third","available":true}],"total":1}}`)
		default:
			writeProbeJSON(response, `{"code":200,"data":{"accounts":[],"total":1}}`)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindConductor, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	view, err := CollectInventory(context.Background(), instance.Id, "account", "")
	require.NoError(t, err)
	page := view.Data.(*InventoryPage)
	require.Equal(t, 3, page.Total)
	require.Len(t, page.Items, 3)
	require.Equal(t, []int64{1, 2, 3}, []int64{page.Items[0].ID, page.Items[1].ID, page.Items[2].ID})
	require.ElementsMatch(t, []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}, requestedOffsets)
}

func TestCollectInventoryAggregatesLifetimeUsageForOldSub2APIAccount(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	createdAt := time.Now().AddDate(0, 0, -120).UTC()
	usagePages := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/admin/accounts":
			writeProbeJSON(response, fmt.Sprintf(`{"code":0,"data":{"items":[{"id":9,"name":"legacy","created_at":%q}],"total":1}}`, createdAt.Format(time.RFC3339)))
		case "/api/v1/admin/usage":
			require.Equal(t, "9", request.URL.Query().Get("account_id"))
			require.Equal(t, createdAt.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02"), request.URL.Query().Get("start_date"))
			require.Equal(t, "Asia/Shanghai", request.URL.Query().Get("timezone"))
			page := request.URL.Query().Get("page")
			usagePages = append(usagePages, page)
			if page == "1" {
				items := make([]map[string]any, usageRecordMaxPageSize)
				for index := range items {
					items[index] = map[string]any{"input_tokens": 2, "output_tokens": 1, "total_cost": 0.01, "account_rate_multiplier": 2}
				}
				encoded, err := json.Marshal(items)
				require.NoError(t, err)
				writeProbeJSON(response, fmt.Sprintf(`{"code":0,"data":{"items":%s,"total":1,"page":1,"page_size":%d}}`, encoded, usageRecordMaxPageSize))
				return
			}
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"input_tokens":5,"output_tokens":3,"cache_read_tokens":2,"actual_cost":0.5}],"total":1,"page":2,"page_size":100}}`)
		case "/api/v1/admin/accounts/9/stats":
			t.Fatal("accounts older than 90 days must use lifetime usage records")
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "admin-secret"})

	view, err := CollectInventory(context.Background(), instance.Id, "account", "")
	require.NoError(t, err)
	page := view.Data.(*InventoryPage)
	require.Len(t, page.Items, 1)
	require.Equal(t, []string{"1", "2"}, usagePages)
	require.Equal(t, float64(usageRecordMaxPageSize+1), *page.Items[0].Requests)
	require.Equal(t, float64(usageRecordMaxPageSize*3+10), *page.Items[0].Tokens)
	require.InDelta(t, float64(usageRecordMaxPageSize)*0.02+0.5, *page.Items[0].Cost, 0.000001)
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
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start := time.Date(2026, time.August, 8, 0, 0, 0, 0, location).Unix()
	end := time.Date(2026, time.August, 9, 12, 0, 0, 0, location).Unix()
	var accountUsageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "admin-secret", request.Header.Get("x-api-key"))
		switch request.URL.Path {
		case "/api/v1/admin/accounts":
			writeProbeJSON(response, `{"code":0,"data":[{"id":9,"name":"upstream-a","status":"active"}]}`)
		case "/api/v1/admin/usage/stats":
			require.Equal(t, "2026-08-08", request.URL.Query().Get("start_date"))
			require.Equal(t, "2026-08-09", request.URL.Query().Get("end_date"))
			require.Equal(t, "Asia/Shanghai", request.URL.Query().Get("timezone"))
			writeProbeJSON(response, `{"code":0,"data":{"total_requests":12,"total_tokens":2000,"total_actual_cost":2}}`)
		case "/api/v1/admin/dashboard/snapshot-v2":
			require.Equal(t, "2026-08-08", request.URL.Query().Get("start_date"))
			require.Equal(t, "2026-08-09", request.URL.Query().Get("end_date"))
			require.Equal(t, "Asia/Shanghai", request.URL.Query().Get("timezone"))
			require.Equal(t, "hour", request.URL.Query().Get("granularity"))
			require.Equal(t, "false", request.URL.Query().Get("include_stats"))
			require.Equal(t, "true", request.URL.Query().Get("include_trend"))
			writeProbeJSON(response, `{"code":0,"data":{"trend":[{"date":"2026-08-07 16:00","requests":7,"total_tokens":1250,"actual_cost":1.25},{"date":"2026-08-08 16:00","requests":5,"total_tokens":750,"actual_cost":0.75}]}}`)
		case "/api/v1/admin/accounts/9/usage", "/api/v1/admin/accounts/today-stats/batch":
			accountUsageRequests.Add(1)
			http.NotFound(response, request)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "admin-secret"})

	view, err := CollectSummary(context.Background(), instance.Id, TimeWindow{Start: start, End: end, Timezone: "Asia/Shanghai"})
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
	require.Zero(t, accountUsageRequests.Load())
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
		case "/api/v1/usage/stats":
			writeProbeJSON(response, `{"code":0,"data":{"total_requests":3,"total_tokens":500,"total_actual_cost":0.25}}`)
		case "/api/v1/usage/dashboard/snapshot-v2":
			require.Equal(t, "true", request.URL.Query().Get("include_trend"))
			require.Equal(t, "hour", request.URL.Query().Get("granularity"))
			writeProbeJSON(response, `{"code":0,"data":{"trend":[{"date":"2026-08-07 16:00","requests":3,"total_tokens":500,"actual_cost":0.25}]}}`)
		case "/api/v1/admin/accounts", "/api/v1/admin/usage/stats", "/api/v1/admin/dashboard/snapshot-v2":
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
	var rpmRequests atomic.Int32
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
		case "/api/log/stat":
			rpmRequests.Add(1)
			writeProbeJSON(response, `{"success":true,"data":{"rpm":23,"tpm":4000}}`)
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

	state, err := RefreshNewAPIRealtime(context.Background(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, 23.0, *state.RPM.Value)
	require.Equal(t, int32(1), rpmRequests.Load())
	realtimeView, err := CollectRealtimeMetrics(context.Background(), instance.Id)
	require.NoError(t, err)
	realtime := realtimeView.Data.(*RealtimeMetricsResult)
	require.Equal(t, 23.0, *realtime.RPM.Value)
	require.Equal(t, "request/min", realtime.RPM.Unit)
	require.Equal(t, int32(1), rpmRequests.Load(), "cache reads must not request the managed instance")
}

func TestNewAPIRealtimeCacheKeepsLastRPMWhenRefreshFails(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/log/stat" {
			http.NotFound(response, request)
			return
		}
		if failing.Load() {
			http.Error(response, "temporary failure", http.StatusBadGateway)
			return
		}
		writeProbeJSON(response, `{"success":true,"data":{"rpm":19}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "metrics-secret", UserID: "1"})

	state, err := RefreshNewAPIRealtime(context.Background(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, 19.0, *state.RPM.Value)

	failing.Store(true)
	state, err = RefreshNewAPIRealtime(context.Background(), instance.Id)
	require.Error(t, err)
	require.True(t, state.Stale)
	require.Equal(t, "reconnecting", state.StreamStatus)
	require.Equal(t, 19.0, *state.RPM.Value)
}

func TestNewAPIRealtimeCacheKeepsLastRPMWhenCredentialBecomesUnavailable(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writeProbeJSON(response, `{"success":true,"data":{"rpm":27}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "metrics-secret", UserID: "1"})

	state, err := RefreshNewAPIRealtime(context.Background(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, 27.0, *state.RPM.Value)
	require.NoError(t, model.DB.Where("instance_id = ?", instance.Id).Delete(&model.ManagedInstanceCredential{}).Error)

	state, err = RefreshNewAPIRealtime(context.Background(), instance.Id)
	require.Error(t, err)
	require.True(t, state.Stale)
	require.Equal(t, "reconnecting", state.StreamStatus)
	require.Equal(t, 27.0, *state.RPM.Value)
}

func TestCollectRealtimeMetricsUsesSub2SnapshotRPM(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/admin/dashboard/snapshot-v2":
			require.Equal(t, "true", request.URL.Query().Get("include_stats"))
			require.Equal(t, "false", request.URL.Query().Get("include_trend"))
			writeProbeJSON(response, `{"code":0,"data":{"stats":{"current_rpm":17}}}`)
		case "/api/v1/admin/groups":
			require.Equal(t, "100", request.URL.Query().Get("page_size"))
			require.Equal(t, "Asia/Shanghai", request.URL.Query().Get("timezone"))
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":49,"account_count":389,"active_account_count":10,"rate_limited_account_count":148}],"total":1,"page":1,"page_size":100,"pages":1}}`)
		case "/api/v1/admin/groups/capacity-summary":
			require.Equal(t, "Asia/Shanghai", request.URL.Query().Get("timezone"))
			writeProbeJSON(response, `{"code":0,"data":[{"group_id":12,"concurrency_used":45,"concurrency_max":120},{"group_id":49,"concurrency_used":48,"concurrency_max":320}]}`)
		case "/api/v1/admin/accounts":
			t.Fatal("group account counts should avoid scanning the account inventory")
		case "/api/v1/admin/usage/stats":
			t.Fatal("realtime metrics must not duplicate the dashboard today-cost request")
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "admin-secret"})

	_, err := RefreshSub2Realtime(context.Background(), instance.Id)
	require.NoError(t, err)
	view, err := CollectRealtimeMetrics(context.Background(), instance.Id)
	require.NoError(t, err)
	realtime := view.Data.(*RealtimeMetricsResult)
	require.Equal(t, 17.0, *realtime.RPM.Value)
	require.Equal(t, 389, realtime.AccountsTotal)
	require.Equal(t, 10, realtime.AccountsAvailable)
	require.Equal(t, 148, realtime.AccountsRateLimited)
	require.Equal(t, 48.0, *realtime.ConcurrencyUsed.Value)
	require.Equal(t, 320.0, *realtime.ConcurrencyMax.Value)
	require.Equal(t, model.ManagedInstanceCollectionSucceeded, realtime.ConcurrencyStatus)
	require.Equal(t, model.ManagedInstanceCollectionSucceeded, realtime.AccountsCollectionStatus)
	require.Nil(t, realtime.TodayCost.Value)
}

func TestCollectRealtimeMetricsVerifiesSub2ZeroRPMWithRecentUsage(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/admin/dashboard/snapshot-v2":
			writeProbeJSON(response, `{"code":0,"data":{"stats":{"current_rpm":0}}}`)
		case "/api/v1/admin/usage":
			writeProbeJSON(response, fmt.Sprintf(`{"code":0,"data":{"items":[{"id":1,"created_at":%d}],"total":1,"page":1,"page_size":100,"pages":1}}`, time.Now().Unix()))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "admin-secret"})

	_, err := RefreshSub2Realtime(context.Background(), instance.Id)
	require.NoError(t, err)
	view, err := CollectRealtimeMetrics(context.Background(), instance.Id)
	require.NoError(t, err)
	realtime := view.Data.(*RealtimeMetricsResult)
	require.Equal(t, 1.0, *realtime.RPM.Value)
}

func TestSub2RealtimeCacheKeepsLastRPMWhenRefreshFails(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	var failing atomic.Bool
	var rpm atomic.Int64
	rpm.Store(9)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if failing.Load() {
			http.Error(response, "temporary failure", http.StatusBadGateway)
			return
		}
		switch request.URL.Path {
		case "/api/v1/admin/dashboard/snapshot-v2":
			writeProbeJSON(response, fmt.Sprintf(`{"code":0,"data":{"stats":{"current_rpm":%d}}}`, rpm.Load()))
		case "/api/v1/admin/accounts":
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":1,"status":"active","schedulable":true}],"total":1,"page":1,"page_size":100,"pages":1}}`)
		case "/api/v1/admin/usage/stats":
			t.Fatal("realtime refresh must not duplicate the dashboard today-cost request")
		case "/api/v1/admin/groups/capacity-summary":
			writeProbeJSON(response, `{"code":0,"data":[{"group_id":49,"concurrency_used":48,"concurrency_max":320}]}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "admin-secret"})

	state, err := RefreshSub2Realtime(context.Background(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, 9.0, *state.RPM.Value)
	require.Equal(t, 1, state.AccountsAvailable)
	require.Nil(t, state.TodayCost.Value)
	require.Equal(t, 48.0, *state.ConcurrencyUsed.Value)
	require.Equal(t, 320.0, *state.ConcurrencyMax.Value)
	rpm.Store(11)
	sub2RealtimeCache.Lock()
	cached := sub2RealtimeCache.states[instance.Id]
	cached.LastAttemptAt = 0
	sub2RealtimeCache.states[instance.Id] = cached
	sub2RealtimeCache.Unlock()
	state, err = RefreshSub2Realtime(context.Background(), instance.Id)
	require.NoError(t, err)
	require.Equal(t, 11.0, *state.RPM.Value)
	view, err := CollectRealtimeMetrics(context.Background(), instance.Id)
	require.NoError(t, err)
	realtime := view.Data.(*RealtimeMetricsResult)
	require.Equal(t, 11.0, *realtime.RPM.Value)

	sub2RealtimeCache.Lock()
	cached = sub2RealtimeCache.states[instance.Id]
	cached.LastDetailsAttemptAt = 0
	sub2RealtimeCache.states[instance.Id] = cached
	sub2RealtimeCache.Unlock()
	failing.Store(true)
	state, err = RefreshSub2Realtime(context.Background(), instance.Id)
	require.Error(t, err)
	require.True(t, state.Stale)
	require.Equal(t, 11.0, *state.RPM.Value)
	require.Equal(t, 1, state.AccountsAvailable)
	require.Nil(t, state.TodayCost.Value)
	require.Equal(t, 48.0, *state.ConcurrencyUsed.Value)
	require.Equal(t, 320.0, *state.ConcurrencyMax.Value)
	require.LessOrEqual(t, state.LastDetailsAttemptAt, time.Now().Unix()-int64(sub2RealtimeDetailsRetryInterval/time.Second))

	view, err = CollectRealtimeMetrics(context.Background(), instance.Id)
	require.NoError(t, err)
	realtime = view.Data.(*RealtimeMetricsResult)
	require.Equal(t, 11.0, *realtime.RPM.Value)
	require.Equal(t, 48.0, *realtime.ConcurrencyUsed.Value)
	require.Equal(t, 320.0, *realtime.ConcurrencyMax.Value)
	require.True(t, realtime.Stale)
	require.Equal(t, "reconnecting", realtime.StreamStatus)
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
			writeProbeJSON(response, `{"code":200,"data":{"accounts":[{"account_id":"101","email":"one@example.com","label":"Primary","auth_type":"oauth","health":"Healthy","available":true,"rpm_current":5},{"account_id":"102","email":"two@example.com","auth_type":"oauth","status":"Paused","available":false,"rpm_current":7}],"total":2}}`)
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

	realtimeView, err := CollectRealtimeMetrics(context.Background(), instance.Id)
	require.NoError(t, err)
	realtime := realtimeView.Data.(*RealtimeMetricsResult)
	require.Equal(t, 12.0, *realtime.RPM.Value)
}

func TestConductorRealtimeRefreshDoesNotQueryTodayCost(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	var failing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if failing.Load() {
			http.Error(response, "temporary failure", http.StatusBadGateway)
			return
		}
		switch request.URL.Path {
		case "/api/v1/auth/login":
			writeProbeJSON(response, `{"code":200,"data":{"token":"conductor-token"}}`)
		case "/api/v1/ws-clients":
			writeProbeJSON(response, `{"code":200,"data":[]}`)
		case "/api/v1/system/quota":
			writeProbeJSON(response, `{"code":200,"data":{"per_account":{"min_interval_ms":400}}}`)
		case "/api/v1/reports/usage":
			t.Fatal("realtime metadata refresh must not duplicate the dashboard today-cost request")
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindConductor, CredentialInput{
		AuthType: "account_password", Secret: "password", UserID: "Cli@mini",
	})
	stream := defaultConductorRealtimeHub.stream(instance.Id, true)
	stream.refreshSources(context.Background())
	stream.mu.Lock()
	state := stream.snapshotLocked()
	stream.mu.Unlock()
	require.Nil(t, state.TodayCost.Value)

	failing.Store(true)
	stream.refreshSources(context.Background())
	stream.mu.Lock()
	state = stream.snapshotLocked()
	stream.mu.Unlock()
	require.Nil(t, state.TodayCost.Value)
}

func TestConductorSummaryUsesReportTotalsAndDailyRows(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/system/health":
			writeProbeJSON(response, `{"code":200,"data":{"status":"ok","accounts_total":2,"accounts_available":2,"accounts_paused":0,"accounts_rejected":0}}`)
		case "/api/v1/reports/usage":
			require.Equal(t, "date", request.URL.Query().Get("group_by"))
			require.Equal(t, "Asia/Shanghai", request.URL.Query().Get("tz"))
			writeProbeJSON(response, `{"code":200,"data":{"rows":[{"date":"2026-08-13","requests":3,"input_tokens":10,"output_tokens":20,"cache_read_tokens":30,"cache_creation_tokens":40,"cost":1.25}],"summary":{"requests":3,"input_tokens":10,"output_tokens":20,"cache_read_tokens":30,"cache_creation_tokens":40,"cost":1.25},"total":1}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindConductor, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	view, err := CollectSummary(context.Background(), instance.Id, TimeWindow{
		Start: time.Date(2026, 8, 12, 0, 0, 0, 0, location).Unix(),
		End:   time.Date(2026, 8, 14, 0, 0, 0, 0, location).Unix(),
	})
	require.NoError(t, err)
	summary := view.Data.(*SummaryResult)
	require.Equal(t, 3.0, *summary.Requests.Value)
	require.Equal(t, 100.0, *summary.Tokens.Value)
	require.Equal(t, 1.25, *summary.Cost.Value)
	require.Len(t, summary.Trend, 3)
	require.Equal(t, "2026-08-13", summary.Trend[1].Date)
	require.Equal(t, 3.0, summary.Trend[1].Requests)
}

func TestConductorAdditionalInventories(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer conductor-secret", request.Header.Get("Authorization"))
		switch request.URL.Path {
		case "/api/v1/users":
			writeProbeJSON(response, `{"code":200,"data":[{"id":1,"username":"root","role":"admin","is_active":true,"created_at":"2026-08-01T10:00:00Z"}]}`)
		case "/api/v1/ws-clients":
			writeProbeJSON(response, `{"code":200,"data":[{"id":2,"name":"worker","url":"ws://worker","enabled":true,"health":"healthy","account_count":4}]}`)
		case "/api/v1/prices":
			writeProbeJSON(response, `{"code":200,"data":[{"id":3,"model":"gpt-5","input_price_per_m":1.2,"output_price_per_m":4.8,"cache_read_price_per_m":0.3,"cache_create_price_per_m":1.5,"note":"current"}]}`)
		case "/api/v1/keys":
			require.Equal(t, "1", request.URL.Query().Get("user_id"))
			writeProbeJSON(response, `{"code":200,"data":[{"id":4,"user_id":1,"key":"must-not-be-exposed","name":"primary-key","is_active":true,"created_at":"2026-08-02T10:00:00Z","settings":{"selection_strategy":"round_robin"}}]}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindConductor, CredentialInput{AuthType: "bearer_pat", Secret: "conductor-secret"})

	users, err := CollectInventory(context.Background(), instance.Id, "user", "")
	require.NoError(t, err)
	userPage := users.Data.(*InventoryPage)
	require.Equal(t, "root", userPage.Items[0].Name)
	require.True(t, *userPage.Items[0].Enabled)

	clients, err := CollectInventory(context.Background(), instance.Id, "ws_client", "")
	require.NoError(t, err)
	clientPage := clients.Data.(*InventoryPage)
	require.Equal(t, 4, *clientPage.Items[0].AccountCount)

	prices, err := CollectInventory(context.Background(), instance.Id, "price", "")
	require.NoError(t, err)
	pricePage := prices.Data.(*InventoryPage)
	require.InDelta(t, 4.8, *pricePage.Items[0].OutputPricePerM, 0.000001)

	keys, err := CollectInventory(context.Background(), instance.Id, "api_key", "")
	require.NoError(t, err)
	keyPage := keys.Data.(*InventoryPage)
	require.Equal(t, "primary-key", keyPage.Items[0].Name)
	require.Equal(t, "root", keyPage.Items[0].Group)
	require.NotContains(t, fmt.Sprint(keyPage.Items[0]), "must-not-be-exposed")
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
