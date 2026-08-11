package managedinstance

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"

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

func TestListUsageRecordsUsesNativeSub2Contract(t *testing.T) {
	newManagedInstanceTestDB(t)
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/v1/admin/usage", request.URL.Path)
		require.Equal(t, "7", request.URL.Query().Get("account_id"))
		require.Equal(t, "stream", request.URL.Query().Get("request_type"))
		require.Equal(t, "sub2-secret", request.Header.Get("x-api-key"))
		writeProbeJSON(response, `{"code":0,"message":"success","data":{"items":[{"id":4,"model":"claude-sonnet-4"}],"total":1,"page":1,"page_size":20,"pages":1}}`)
	}))
	defer server.Close()
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindSub2API, CredentialInput{AuthType: "admin_token", Secret: "sub2-secret"})

	page, err := ListUsageRecords(context.Background(), instance.Id, url.Values{"account_id": {"7"}, "request_type": {"stream"}})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceKindSub2API, page.Kind)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Items, 1)
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

	page, err := ListUsageRecords(context.Background(), instance.Id, nil)
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
				response.WriteHeader(http.StatusUnauthorized)
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
