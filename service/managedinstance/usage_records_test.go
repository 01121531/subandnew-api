package managedinstance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestNormalizeUsageRecordQueryKeepsOnlyNativeFilters(t *testing.T) {
	query, err := normalizeUsageRecordQuery(model.ManagedInstanceKindNewAPI, url.Values{
		"p":                   {"2"},
		"page_size":           {"50"},
		"username":            {"alice"},
		"upstream_request_id": {"upstream-1"},
		"unknown":             {"discarded"},
	})
	require.NoError(t, err)
	require.Equal(t, "2", query.Get("p"))
	require.Equal(t, "50", query.Get("page_size"))
	require.Equal(t, "alice", query.Get("username"))
	require.Equal(t, "upstream-1", query.Get("upstream_request_id"))
	require.Empty(t, query.Get("unknown"))

	multi, err := normalizeUsageRecordQuery(model.ManagedInstanceKindNewAPI, url.Values{
		"username": {"alice", "bob", "alice"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"alice", "bob"}, multi["username"])

	_, err = normalizeUsageRecordQuery(model.ManagedInstanceKindSub2API, url.Values{"start_date": {"2026/08/09"}})
	require.ErrorIs(t, err, ErrInvalidInstance)
	_, err = normalizeUsageRecordQuery(model.ManagedInstanceKindNewAPI, url.Values{"channel": {"abc"}})
	require.ErrorIs(t, err, ErrInvalidInstance)
	_, err = normalizeUsageRecordQuery(model.ManagedInstanceKindNewAPI, url.Values{
		"start_timestamp": {"200"}, "end_timestamp": {"100"},
	})
	require.ErrorIs(t, err, ErrInvalidInstance)
	_, err = normalizeUsageRecordQuery(model.ManagedInstanceKindSub2API, url.Values{
		"start_date": {"2026-08-10"}, "end_date": {"2026-08-09"},
	})
	require.ErrorIs(t, err, ErrInvalidInstance)
	_, err = normalizeUsageRecordQuery(model.ManagedInstanceKindSub2API, url.Values{"timezone": {"not/a-zone"}})
	require.ErrorIs(t, err, ErrInvalidInstance)
}

func TestListUsageRecordsCombinesMultipleFilterValues(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		username := request.URL.Query().Get("username")
		require.Contains(t, []string{"alice", "bob"}, username)
		require.Len(t, request.URL.Query()["username"], 1)
		id := 1
		createdAt := 100
		if username == "bob" {
			id = 2
			createdAt = 200
		}
		writeProbeJSON(response, fmt.Sprintf(`{"success":true,"data":{"items":[{"id":%d,"username":%q,"created_at":%d}],"total":1,"page":1,"page_size":20}}`, id, username, createdAt))
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	page, err := ListUsageRecords(context.Background(), instance.Id, url.Values{
		"username": {"alice", "bob"}, "p": {"1"}, "page_size": {"20"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Len(t, page.Items, 2)
	require.Contains(t, string(page.Items[0]), `"username":"bob"`)
}

func TestUsageRecordFilterOptionsComeFromNativeUsageFields(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/v1/admin/usage", request.URL.Path)
		require.Equal(t, "100", request.URL.Query().Get("page_size"))
		writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":4,"user_id":11,"user":{"email":"user@example.com"},"api_key_id":12,"api_key":{"name":"primary"},"account_id":13,"account":{"name":"openai-main"},"group_id":14,"group":{"name":"default"},"model":"gpt-5","request_id":"req-1"}],"total":1,"page":1,"page_size":100}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "secret"})

	options, err := GetUsageRecordFilterOptions(context.Background(), instance.Id, nil)
	require.NoError(t, err)
	require.Equal(t, "11", options.Fields["user_id"][0].Value)
	require.Equal(t, "user@example.com (#11)", options.Fields["user_id"][0].Label)
	require.Equal(t, "gpt-5", options.Fields["model"][0].Value)
}

func TestListUsageRecordsUsesNativeNewAPIContract(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/log/", request.URL.Path)
		require.Equal(t, "2", request.URL.Query().Get("p"))
		require.Equal(t, "alice", request.URL.Query().Get("username"))
		require.Equal(t, "Bearer new-api-secret", request.Header.Get("Authorization"))
		writeProbeJSON(response, `{"success":true,"data":{"items":[{"id":9,"username":"alice","model_name":"gpt-5"}],"total":21,"page":2,"page_size":20}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "new-api-secret"})

	page, err := ListUsageRecords(context.Background(), instance.Id, url.Values{"p": {"2"}, "username": {"alice"}})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindNewAPI, page.Kind)
	require.Equal(t, int64(21), page.Total)
	require.Len(t, page.Items, 1)
}

func TestNewAPIAccountPasswordReusesSessionAcrossUsageRequests(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	var loginCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/user/login":
			loginCount.Add(1)
			http.SetCookie(response, &http.Cookie{Name: "session", Value: "shared-session", Path: "/"})
			writeProbeJSON(response, `{"success":true,"data":{"id":7}}`)
		case "/api/log/":
			cookie, err := request.Cookie("session")
			require.NoError(t, err)
			require.Equal(t, "shared-session", cookie.Value)
			require.Equal(t, "7", request.Header.Get("New-Api-User"))
			writeProbeJSON(response, `{"success":true,"data":{"items":[],"total":0,"page":1,"page_size":20}}`)
		case "/api/data/":
			cookie, err := request.Cookie("session")
			require.NoError(t, err)
			require.Equal(t, "shared-session", cookie.Value)
			writeProbeJSON(response, `{"success":true,"data":[]}`)
		case "/api/status":
			writeProbeJSON(response, `{"success":true,"data":{"quota_per_unit":500000}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{
		AuthType: "account_password", Secret: "password", UserID: "admin",
	})

	_, err := ListUsageRecords(context.Background(), instance.Id, nil)
	require.NoError(t, err)
	_, err = GetUsageRecordSummary(context.Background(), instance.Id, nil)
	require.NoError(t, err)
	require.Equal(t, int32(1), loginCount.Load())
}

func TestNewAPIAccountPasswordRefreshesRejectedSession(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	var loginCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/user/login":
			login := loginCount.Add(1)
			http.SetCookie(response, &http.Cookie{Name: "session", Value: fmt.Sprintf("session-%d", login), Path: "/"})
			writeProbeJSON(response, `{"success":true,"data":{"id":7}}`)
		case "/api/log/":
			cookie, err := request.Cookie("session")
			require.NoError(t, err)
			if cookie.Value == "session-1" {
				writeProbeJSON(response, `{"success":false,"message":"session expired"}`)
				return
			}
			require.Equal(t, "session-2", cookie.Value)
			writeProbeJSON(response, `{"success":true,"data":{"items":[],"total":0,"page":1,"page_size":20}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{
		AuthType: "account_password", Secret: "password", UserID: "admin",
	})

	_, err := ListUsageRecords(context.Background(), instance.Id, nil)
	require.NoError(t, err)
	require.Equal(t, int32(2), loginCount.Load())
}

func TestListUsageRecordsUsesNativeSub2Contract(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/v1/admin/usage", request.URL.Path)
		require.Equal(t, "7", request.URL.Query().Get("account_id"))
		require.Equal(t, "stream", request.URL.Query().Get("request_type"))
		require.Empty(t, request.URL.Query().Get("exact_total"))
		require.Equal(t, "sub2-secret", request.Header.Get("x-api-key"))
		writeProbeJSON(response, `{"code":0,"message":"success","data":{"items":[{"id":4,"model":"claude-sonnet-4"}],"total":1,"page":1,"page_size":20,"pages":1}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "sub2-secret"})

	page, err := ListUsageRecords(context.Background(), instance.Id, url.Values{"account_id": {"7"}, "request_type": {"stream"}, "exact_total": {"true"}})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindSub2API, page.Kind)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Items, 1)
}

func TestConductorUsageRecordsSummaryOptionsAndSessionReuse(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	var loginCount atomic.Int32
	var usageCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1/auth/login" {
			loginCount.Add(1)
			writeProbeJSON(response, `{"code":200,"data":{"token":"conductor-token"}}`)
			return
		}
		require.Equal(t, "Bearer conductor-token", request.Header.Get("Authorization"))
		switch request.URL.Path {
		case "/api/v1/users":
			writeProbeJSON(response, `{"code":200,"data":[{"id":7,"username":"alice","role":"admin","is_active":true}]}`)
		case "/api/v1/prices":
			writeProbeJSON(response, `{"code":200,"data":[{"id":1,"model":"gpt-5","input_price_per_m":1,"output_price_per_m":2,"cache_read_price_per_m":0.5,"cache_create_price_per_m":1,"note":"test"}]}`)
		case "/api/v1/system/health":
			writeProbeJSON(response, `{"code":200,"data":{"status":"ok","accounts_total":2,"accounts_available":1,"accounts_paused":1,"accounts_rejected":0}}`)
		case "/api/v1/usage":
			usageCount.Add(1)
			require.Equal(t, "2026-08-07", request.URL.Query().Get("from"))
			require.Equal(t, "2026-08-13", request.URL.Query().Get("to"))
			require.Equal(t, "Asia/Shanghai", request.URL.Query().Get("timezone"))
			require.Equal(t, "model", request.URL.Query().Get("group_by"))
			writeProbeJSON(response, `{"code":200,"data":{"labels":{"7":"alice"},"total":1,"usage":{"7":{"2026-08-12":{"gpt-5":{"requests":3,"input_tokens":1000000,"output_tokens":500000,"cache_read_tokens":100000,"cache_creation_tokens":50000,"cache_5m_tokens":30000,"cache_1h_tokens":20000},"_total":{"requests":3}}}}}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindConductor, CredentialInput{
		AuthType: "account_password", Secret: "password", UserID: "admin",
	})
	filters := url.Values{
		"start_date": {"2026-08-07"}, "end_date": {"2026-08-13"}, "timezone": {"Asia/Shanghai"},
		"page": {"1"}, "page_size": {"20"}, "user_id": {"7"}, "model": {"gpt-5"},
	}

	page, err := ListUsageRecords(context.Background(), instance.Id, filters)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindConductor, page.Kind)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Items, 1)
	var row conductorUsageRow
	require.NoError(t, json.Unmarshal(page.Items[0], &row))
	require.Equal(t, 3.0, row.Requests)
	require.Equal(t, 1650000.0, row.TotalTokens)
	require.InDelta(t, 2.1, row.ActualCost, 0.000001)

	summary, err := GetUsageRecordSummary(context.Background(), instance.Id, filters)
	require.NoError(t, err)
	require.Equal(t, "USD", summary.Currency)
	require.Equal(t, 1650000.0, summary.TotalTokens)
	require.InDelta(t, 2.1, summary.Amount, 0.000001)

	options, err := GetUsageRecordFilterOptions(context.Background(), instance.Id, filters)
	require.NoError(t, err)
	require.Equal(t, "7", options.Fields["user_id"][0].Value)
	require.Equal(t, "gpt-5", options.Fields["model"][0].Value)
	require.Equal(t, int32(1), loginCount.Load())

	headers, fields := usageCSVSchema(model.ManagedInstanceKindConductor)
	require.Len(t, headers, 13)
	cells, err := usageCSVRow(page.Items[0], fields)
	require.NoError(t, err)
	require.Equal(t, "gpt-5", cells[3])
	require.Equal(t, "2.1", cells[12])

	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	view, err := CollectSummary(context.Background(), instance.Id, TimeWindow{
		Start: time.Date(2026, 8, 7, 0, 0, 0, 0, location).Unix(),
		End:   time.Date(2026, 8, 13, 0, 0, 0, 0, location).Unix(),
	})
	require.NoError(t, err)
	dashboard := view.Data.(*SummaryResult)
	require.Equal(t, 3.0, *dashboard.Requests.Value)
	require.Equal(t, 1650000.0, *dashboard.Tokens.Value)
	require.InDelta(t, 2.1, *dashboard.Cost.Value, 0.000001)
	require.Len(t, dashboard.Trend, 7)
	require.Equal(t, "2026-08-07", dashboard.Trend[0].Date)
	require.Equal(t, "2026-08-12", dashboard.Trend[5].Date)
	require.Equal(t, 3.0, dashboard.Trend[5].Requests)
	require.Equal(t, int32(3), usageCount.Load())
}

func TestCollectConductorKeyUsageUsesKeyReportWithoutExposingSecret(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/v1/reports/keys", request.URL.Path)
		require.Equal(t, "total", request.URL.Query().Get("group_by"))
		require.Equal(t, "2", request.URL.Query().Get("key_id"))
		require.Equal(t, "Asia/Shanghai", request.URL.Query().Get("tz"))
		writeProbeJSON(response, `{"code":200,"data":{"rows":[{"key_id":2,"key_name":"primary","user_id":1,"username":"root","requests":4,"input_tokens":10,"output_tokens":20,"cache_read_tokens":30,"cache_creation_tokens":40,"cost":0.5}],"summary":{"requests":4},"total":1}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindConductor, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})
	result, err := CollectConductorKeyUsage(context.Background(), instance.Id, 2, TimeWindow{Start: 1_786_032_000, End: 1_786_636_800}, "Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, int64(2), result.KeyID)
	require.Equal(t, "primary", result.KeyName)
	require.Equal(t, 4.0, result.TotalRequests)
	require.Equal(t, 100.0, result.TotalTokens)
	require.Equal(t, 0.5, result.Amount)
	require.NotContains(t, fmt.Sprint(result), "secret")
}

func TestRegularSub2AccountUsesUserUsageEndpoints(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/auth/login":
			writeProbeJSON(response, `{"code":0,"data":{"access_token":"user-token"}}`)
		case "/api/v1/usage":
			require.Equal(t, "Bearer user-token", request.Header.Get("Authorization"))
			require.Empty(t, request.URL.Query().Get("exact_total"))
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"id":4,"model":"claude-sonnet-4"}],"total":1,"page":1,"page_size":20,"pages":1}}`)
		case "/api/v1/usage/dashboard/snapshot-v2":
			require.Equal(t, "true", request.URL.Query().Get("include_trend"))
			writeProbeJSON(response, `{"code":0,"data":{"trend":[{"total_tokens":30,"actual_cost":0.5}]}}`)
		case "/api/v1/admin/usage", "/api/v1/admin/dashboard/snapshot-v2":
			t.Fatal("regular account usage must not call an administrator endpoint")
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{
		AuthType: "account_password", AccessScope: model.ManagedInstanceAccessUser,
		Secret: "password", UserID: "user@example.com",
	})

	page, err := ListUsageRecords(context.Background(), instance.Id, url.Values{"exact_total": {"true"}})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)

	summary, err := GetUsageRecordSummary(context.Background(), instance.Id, url.Values{
		"start_date": {"2026-08-01"}, "end_date": {"2026-08-07"}, "timezone": {"Asia/Shanghai"},
	})
	require.NoError(t, err)
	require.Equal(t, 30.0, summary.TotalTokens)
	require.Equal(t, 0.5, summary.Amount)
}

func TestSub2AccountPasswordReusesAndRefreshesConcurrentSession(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	var loginCount atomic.Int32
	var currentToken atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/auth/login":
			token := loginCount.Add(1)
			currentToken.Store(token)
			writeProbeJSON(response, fmt.Sprintf(`{"code":0,"data":{"access_token":"token-%d"}}`, token))
		case "/api/v1/admin/usage", "/api/v1/admin/dashboard/snapshot-v2":
			expected := fmt.Sprintf("Bearer token-%d", currentToken.Load())
			if request.Header.Get("Authorization") != expected {
				writeProbeJSON(response, `{"code":401,"message":"token expired"}`)
				return
			}
			if request.URL.Path == "/api/v1/admin/usage" {
				writeProbeJSON(response, `{"code":0,"data":{"items":[],"total":0,"page":1,"page_size":20}}`)
				return
			}
			writeProbeJSON(response, `{"code":0,"data":{"trend":[]}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{
		AuthType: "account_password", AccessScope: model.ManagedInstanceAccessAdmin,
		Secret: "password", UserID: "channel@example.com",
	})

	const requestCount = 12
	errors := make(chan error, requestCount)
	var requests sync.WaitGroup
	for index := range requestCount {
		requests.Add(1)
		go func() {
			defer requests.Done()
			if index%2 == 0 {
				_, err := ListUsageRecords(context.Background(), instance.Id, nil)
				errors <- err
				return
			}
			_, err := GetUsageRecordSummary(context.Background(), instance.Id, url.Values{
				"start_date": {"2026-08-10"}, "end_date": {"2026-08-11"}, "timezone": {"Asia/Shanghai"},
			})
			errors <- err
		}()
	}
	requests.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), loginCount.Load())

	currentToken.Store(99)
	_, err := ListUsageRecords(context.Background(), instance.Id, nil)
	require.NoError(t, err)
	require.Equal(t, int32(2), loginCount.Load())
}

func TestGetUsageRecordSummaryUsesNativeNewAPIData(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/data/":
			require.Equal(t, "100", request.URL.Query().Get("start_timestamp"))
			require.Equal(t, "200", request.URL.Query().Get("end_timestamp"))
			require.Equal(t, "Bearer new-api-secret", request.Header.Get("Authorization"))
			writeProbeJSON(response, `{"success":true,"data":[{"token_used":30,"quota":250000},{"token_used":12,"quota":500000}]}`)
		case "/api/status":
			writeProbeJSON(response, `{"success":true,"data":{"quota_per_unit":500000}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "new-api-secret"})

	summary, err := GetUsageRecordSummary(context.Background(), instance.Id, url.Values{
		"start_timestamp": {"100"}, "end_timestamp": {"200"},
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindNewAPI, summary.Kind)
	require.Equal(t, 42.0, summary.TotalTokens)
	require.Equal(t, 1.5, summary.Amount)
	require.Equal(t, "USD", summary.Currency)
}

func TestGetUsageRecordSummaryUsesNativeSub2Dashboard(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/v1/admin/dashboard/snapshot-v2", request.URL.Path)
		require.Equal(t, "2026-08-01", request.URL.Query().Get("start_date"))
		require.Equal(t, "2026-08-07", request.URL.Query().Get("end_date"))
		require.Equal(t, "Asia/Shanghai", request.URL.Query().Get("timezone"))
		require.Equal(t, "hour", request.URL.Query().Get("granularity"))
		require.Equal(t, "false", request.URL.Query().Get("include_stats"))
		require.Equal(t, "sub2-secret", request.Header.Get("x-api-key"))
		writeProbeJSON(response, `{"code":0,"data":{"trend":[{"total_tokens":10,"actual_cost":0.15},{"total_tokens":20,"actual_cost":0.35}]}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "sub2-secret"})

	summary, err := GetUsageRecordSummary(context.Background(), instance.Id, url.Values{
		"start_date": {"2026-08-01"}, "end_date": {"2026-08-07"}, "timezone": {"Asia/Shanghai"},
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindSub2API, summary.Kind)
	require.Equal(t, 30.0, summary.TotalTokens)
	require.InDelta(t, 0.5, summary.Amount, 0.000001)
	require.Equal(t, "USD", summary.Currency)
}

func TestGetUsageRecordSummaryAggregatesFilteredSub2Records(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/v1/admin/usage", request.URL.Path)
		require.Equal(t, "7", request.URL.Query().Get("account_id"))
		require.Equal(t, "claude-sonnet", request.URL.Query().Get("model"))
		require.Equal(t, "100", request.URL.Query().Get("page_size"))
		require.Empty(t, request.URL.Query().Get("exact_total"))
		switch request.URL.Query().Get("page") {
		case "1":
			writeProbeJSON(response, `{"code":0,"data":{"items":[{"input_tokens":10,"output_tokens":5,"cache_read_tokens":2,"cache_creation_tokens":1,"actual_cost":0.25},{"input_tokens":7,"output_tokens":3,"actual_cost":0.75}],"total":1,"page":1,"page_size":100}}`)
		case "2":
			writeProbeJSON(response, `{"code":0,"data":{"items":[],"total":1,"page":2,"page_size":100}}`)
		default:
			t.Fatalf("unexpected usage page %s", request.URL.Query().Get("page"))
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "sub2-secret"})

	summary, err := GetUsageRecordSummary(context.Background(), instance.Id, url.Values{
		"start_date": {"2026-08-01"}, "end_date": {"2026-08-07"}, "timezone": {"Asia/Shanghai"},
		"account_id": {"7"}, "model": {"claude-sonnet"},
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindSub2API, summary.Kind)
	require.Equal(t, 28.0, summary.TotalTokens)
	require.InDelta(t, 1.0, summary.Amount, 0.000001)
	require.Equal(t, "USD", summary.Currency)
}

func TestGetUsageRecordSummaryAggregatesFilteredNewAPIRecords(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/log/":
			require.Equal(t, "alice", request.URL.Query().Get("username"))
			require.Equal(t, "100", request.URL.Query().Get("page_size"))
			switch request.URL.Query().Get("p") {
			case "1":
				writeProbeJSON(response, `{"success":true,"data":{"items":[{"prompt_tokens":10,"completion_tokens":5,"quota":500000}],"total":1,"page":1,"page_size":100}}`)
			case "2":
				writeProbeJSON(response, `{"success":true,"data":{"items":[],"total":1,"page":2,"page_size":100}}`)
			default:
				t.Fatalf("unexpected usage page %s", request.URL.Query().Get("p"))
			}
		case "/api/status":
			writeProbeJSON(response, `{"success":true,"data":{"quota_per_unit":500000}}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "new-api-secret"})

	summary, err := GetUsageRecordSummary(context.Background(), instance.Id, url.Values{
		"start_timestamp": {"100"}, "end_timestamp": {"200"}, "username": {"alice"},
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindNewAPI, summary.Kind)
	require.Equal(t, 15.0, summary.TotalTokens)
	require.InDelta(t, 1.0, summary.Amount, 0.000001)
	require.Equal(t, "USD", summary.Currency)
}

func TestUsageCSVCellPreventsFormulaInjection(t *testing.T) {
	row, err := usageCSVRow([]byte(`{"username":"=HYPERLINK(\"https://example.invalid\")","quota":-12,"content":" @SUM(1,1)"}`), []usageCSVField{
		field("username"), field("quota"), field("content"),
	})
	require.NoError(t, err)
	require.Equal(t, `'=HYPERLINK("https://example.invalid")`, row[0])
	require.Equal(t, "-12", row[1])
	require.Equal(t, "' @SUM(1,1)", row[2])
}

func TestUsageCSVRowCalculatesSub2AccountBilledCost(t *testing.T) {
	row, err := usageCSVRow([]byte(`{"total_cost":1.25,"account_stats_cost":2,"account_rate_multiplier":1.5}`), []usageCSVField{
		derivedField("account_billed_cost"),
	})
	require.NoError(t, err)
	require.Equal(t, "3.000000", row[0])

	row, err = usageCSVRow([]byte(`{"total_cost":1.25}`), []usageCSVField{
		derivedField("account_billed_cost"),
	})
	require.NoError(t, err)
	require.Equal(t, "1.250000", row[0])
}

func TestUsageRecordExportReportsProcessedRows(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeProbeJSON(response, `{"success":true,"data":{"items":[{"id":1,"username":"one"},{"id":2,"username":"two"}],"total":2,"page":1,"page_size":100}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})
	export, err := PrepareUsageRecordsCSV(context.Background(), instance.Id, nil)
	require.NoError(t, err)

	progress := make([]UsageRecordExportProgress, 0, 3)
	var output bytes.Buffer
	count, err := export.WriteWithProgress(context.Background(), &output, func(value UsageRecordExportProgress) error {
		progress = append(progress, value)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.NotEmpty(t, output.Bytes())
	require.Equal(t, UsageRecordExportProgress{Progress: 0, Processed: 0, Total: 2, Stage: "exporting"}, progress[0])
	require.Equal(t, UsageRecordExportProgress{Progress: 100, Processed: 2, Total: 2, Stage: "completed"}, progress[len(progress)-1])
}

func TestUsageRecordTaskExportPersistsForRepeatedDownloads(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	t.Setenv("MANAGED_USAGE_EXPORT_DIR", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeProbeJSON(response, `{"success":true,"data":{"items":[{"id":1,"username":"one"}],"total":1,"page":1,"page_size":100}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})
	taskID := "systask_persistent_export"

	artifact, err := ExportUsageRecordsCSVToTaskFile(context.Background(), instance.Id, taskID, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, artifact.RecordCount)
	require.Greater(t, artifact.ExpiresAt, time.Now().Add(29*24*time.Hour).Unix())

	partPath, err := usageRecordExportTaskPath(taskID, true)
	require.NoError(t, err)
	_, err = os.Stat(partPath)
	require.ErrorIs(t, err, os.ErrNotExist)
	for range 2 {
		file, openErr := OpenUsageRecordExportArtifact(taskID)
		require.NoError(t, openErr)
		info, statErr := file.Stat()
		require.NoError(t, statErr)
		require.Equal(t, artifact.Size, info.Size())
		require.NoError(t, file.Close())
	}
}

func TestUsageRecordExportContinuesWhenUpstreamCapsPageSize(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		page, err := strconv.Atoi(request.URL.Query().Get("p"))
		require.NoError(t, err)
		require.Equal(t, "100", request.URL.Query().Get("page_size"))
		start := (page - 1) * 20
		end := start + 20
		if end > 45 {
			end = 45
		}
		items := make([]map[string]any, 0, end-start)
		for id := start + 1; id <= end; id++ {
			items = append(items, map[string]any{"id": id, "username": fmt.Sprintf("user-%d", id)})
		}
		response.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(response).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"items": items, "total": 45, "page": page, "page_size": 20},
		}))
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	var output bytes.Buffer
	count, err := StreamUsageRecordsCSV(context.Background(), instance.Id, nil, &output)
	require.NoError(t, err)
	require.Equal(t, 45, count)
	require.Equal(t, 3, requests)
	require.Contains(t, output.String(), "user-45")
}

func TestUsageRecordExportRejectsPrematureEmptyPage(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		page := integerValue(request.URL.Query().Get("p"), 1)
		items := []map[string]any{{"id": 1, "username": "first"}}
		if page > 1 {
			items = []map[string]any{}
		}
		response.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(response).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"items": items, "total": 2, "page": page, "page_size": 1},
		}))
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	var output bytes.Buffer
	count, err := StreamUsageRecordsCSV(context.Background(), instance.Id, nil, &output)
	require.ErrorIs(t, err, ErrUsageExportIncomplete)
	require.Equal(t, 1, count)
}

func TestStreamUsageRecordsCSVRejectsOversizedExportBeforeWriting(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeProbeJSON(response, `{"success":true,"data":{"items":[],"total":1000001,"page":1,"page_size":100}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindNewAPI, CredentialInput{AuthType: "bearer_pat", Secret: "secret"})

	var output bytes.Buffer
	count, err := StreamUsageRecordsCSV(context.Background(), instance.Id, nil, &output)
	require.ErrorIs(t, err, ErrUsageExportTooLarge)
	require.Zero(t, count)
	require.Zero(t, output.Len())
}

func TestSub2ExportRequestsExactTotalBeforeWriting(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "true", request.URL.Query().Get("exact_total"))
		writeProbeJSON(response, `{"code":0,"data":{"items":[],"total":1000001,"page":1,"page_size":100}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "secret"})

	var output bytes.Buffer
	count, err := StreamUsageRecordsCSV(context.Background(), instance.Id, nil, &output)
	require.ErrorIs(t, err, ErrUsageExportTooLarge)
	require.Zero(t, count)
	require.Zero(t, output.Len())
}
