package managedinstance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestDecodeClaudeGatewayAccountCostDetail(t *testing.T) {
	detail, err := decodeClaudeGatewayAccountCostDetail([]byte(`{
		"data":{"account":{"total_cost":"13000.12345678","created_at":"2026-08-31T19:51:42.314Z","usage_records":{"today":{"cost":"160","requests":15116,"tokens":"47175232"}}}},
		"cost_windows":{"cost_30d":"13160.12345678"}
	}`))
	require.NoError(t, err)
	require.InDelta(t, 13000.12345678, *detail.LifetimeCost, 1e-10)
	require.InDelta(t, 160, *detail.TodayCost, 1e-10)
	require.Equal(t, float64(15116), *detail.TodayRequests)
	require.Equal(t, float64(47175232), *detail.TodayTokens)
	require.Equal(t, "detail", detail.LifetimeSource)
	require.InDelta(t, 13160.12345678, *detail.detailCost30D, 1e-10)
	require.Equal(t, int64(1788205902), detail.accountCreatedAt)
}

func TestFetchClaudeGatewayAccountCostUsesThirtyDayTotalForNewAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeProbeJSON(response, `{
			"account":{"id":"account-1","total_cost":"0.0000"},
			"stats":{"daily_req":42724,"daily_tok":844606911,"daily_cost":7576.71379905},
			"cost_windows":{"cost_30d":7576.713798}
		}`)
	}))
	t.Cleanup(server.Close)
	connector := testAccountCostConnector(t, server.URL)
	result, err := fetchClaudeGatewayAccountCostDetail(context.Background(), connector, &CredentialMaterial{AuthType: "bearer_pat", Secret: "secret"}, AccountExportSelection{
		InstanceID: 1, InstanceKind: model.ManagedInstanceKindClaudeGateway,
		Account: InventoryItem{ID: 1, IDText: "account-1", CreatedAt: time.Now().Add(-time.Hour).Unix()},
	})
	require.NoError(t, err)
	require.Equal(t, "detail_cost_30d", result.LifetimeSource)
	require.InDelta(t, 7576.713798, *result.LifetimeCost, 1e-10)
	require.Zero(t, *result.CostExcludingToday)
	require.Equal(t, float64(42724), *result.TodayRequests)
	require.Equal(t, float64(844606911), *result.TodayTokens)
}

func TestFetchClaudeGatewayAccountCostUsesFrozenListTotalForOldAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		writeProbeJSON(response, `{
			"account":{"id":"account-1","created_at":"2026-06-01T00:00:00+08:00","total_cost":"0.0000"},
			"stats":{"daily_cost":2},
			"cost_windows":{"cost_30d":30}
		}`)
	}))
	t.Cleanup(server.Close)
	connector := testAccountCostConnector(t, server.URL)
	lifetime := 100.0
	result, err := fetchClaudeGatewayAccountCostDetail(context.Background(), connector, &CredentialMaterial{AuthType: "bearer_pat", Secret: "secret"}, AccountExportSelection{
		InstanceID: 1, InstanceKind: model.ManagedInstanceKindClaudeGateway,
		Account: InventoryItem{ID: 1, IDText: "account-1", CreatedAt: time.Now().Add(-60 * 24 * time.Hour).Unix(), LifetimeCost: &lifetime},
	})
	require.NoError(t, err)
	require.Equal(t, "account_list", result.LifetimeSource)
	require.InDelta(t, 100, *result.LifetimeCost, 1e-10)
	require.InDelta(t, 98, *result.CostExcludingToday, 1e-10)
}

func TestAccountCostHTTPErrorHonorsRetryAfterCap(t *testing.T) {
	header := http.Header{}
	header.Set("Retry-After", "120")
	detailErr := accountCostHTTPError(&ConnectorResponse{StatusCode: http.StatusTooManyRequests, Header: header})
	require.True(t, detailErr.retryable)
	require.Equal(t, "account_cost_rate_limited", detailErr.code)
	require.Equal(t, 30*time.Second, accountCostExportRetryDelay(1, detailErr.retryAfter))
}

func TestAccountCostExportRetriesAndWritesPartialWorkbook(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.ManagedUsageExport{}, &model.ManagedExportItem{}))
	t.Setenv("MANAGED_USAGE_EXPORT_DIR", t.TempDir())
	t.Setenv(managedInstanceAllowedCIDRsEnv, "127.0.0.0/8")

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/admin/oauth-accounts/b766807a-1d82-47cf-9734-27e033c909be", request.URL.Path)
		if attempts.Add(1) < accountCostExportMaxAttempts {
			response.WriteHeader(http.StatusBadGateway)
			writeProbeJSON(response, `{"message":"temporary"}`)
			return
		}
		writeProbeJSON(response, `{"total_cost":"13000.12345678","stats":{"daily_cost":"160","daily_req":15116,"daily_tok":47175232}}`)
	}))
	t.Cleanup(server.Close)
	instance := createProbeInstance(t, server.URL, model.ManagedInstanceKindClaudeGateway, CredentialInput{AuthType: "bearer_pat", Secret: "test-token"})

	selection := AccountExportSelection{
		InstanceID: instance.Id, InstanceName: instance.Name, InstanceKind: model.ManagedInstanceKindClaudeGateway,
		SourceName: "direct", Account: InventoryItem{
			ID: 1, IDText: "b766807a-1d82-47cf-9734-27e033c909be", Name: "allen",
			Email: "account@example.com", VendorName: "vendor-a", VendorEmail: "vendor@example.com",
		},
	}
	metadata, err := json.Marshal(selection)
	require.NoError(t, err)
	record := &model.ManagedUsageExport{ActorID: 1, ActorName: "admin", ExportKind: model.ManagedExportKindAccountCosts, FileFormat: model.ManagedExportFormatXLSX, Source: "inventory"}
	task, err := model.CreateManagedUsageExportWithItems(record, map[string]any{"report_type": "account_costs"}, map[string]any{}, []*model.ManagedExportItem{{InstanceID: instance.Id, ResourceID: 1, Metadata: string(metadata)}})
	require.NoError(t, err)

	previousDelay := accountCostExportRetryDelay
	accountCostExportRetryDelay = func(int, time.Duration) time.Duration { return time.Millisecond }
	t.Cleanup(func() { accountCostExportRetryDelay = previousDelay })
	artifact, err := ExportClaudeGatewayAccountCostsXLSXToTaskFile(context.Background(), task.TaskID, 1, "zh-CN", nil)
	require.NoError(t, err)
	require.Equal(t, 1, artifact.RecordCount)

	items, err := model.ListManagedExportItems(task.TaskID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int32(accountCostExportMaxAttempts), attempts.Load())
	require.Zero(t, artifact.WarningCount)
	require.Equal(t, model.ManagedExportItemStatusSucceeded, items[0].Status)
	require.Equal(t, accountCostExportMaxAttempts, items[0].Attempts)

	path, _, err := accountExportTaskPaths(task.TaskID)
	require.NoError(t, err)
	workbook, err := excelize.OpenFile(path, excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = workbook.Close() })
	require.Equal(t, "账号 ID", accountCostCell(t, workbook, "B1"))
	require.Equal(t, "b766807a-1d82-47cf-9734-27e033c909be", accountCostCell(t, workbook, "B2"))
	require.Equal(t, "供应商", accountCostCell(t, workbook, "F1"))
	require.Equal(t, "vendor-a", accountCostCell(t, workbook, "F2"))
	require.Equal(t, "13000.12345678", accountCostCell(t, workbook, "H2"))
	require.Equal(t, "160", accountCostCell(t, workbook, "I2"))
	require.Equal(t, "12840.12345678", accountCostCell(t, workbook, "J2"))
	require.Equal(t, "15116", accountCostCell(t, workbook, "K2"))
	require.Equal(t, "47175232", accountCostCell(t, workbook, "L2"))
	require.Equal(t, "成功", accountCostCell(t, workbook, "N2"))
}

func TestWriteAccountCostExportWorkbookKeepsFailedRows(t *testing.T) {
	t.Setenv("MANAGED_USAGE_EXPORT_DIR", t.TempDir())
	selection := AccountExportSelection{InstanceID: 6, InstanceName: "gateway", InstanceKind: model.ManagedInstanceKindClaudeGateway, Account: InventoryItem{ID: 2, IDText: "missing-account", Name: "missing"}}
	metadata, err := json.Marshal(selection)
	require.NoError(t, err)
	artifact, err := writeAccountCostExportWorkbook("systask_account_cost_warning", "zh-CN", []*model.ManagedExportItem{{
		Status: model.ManagedExportItemStatusFailed, Attempts: 4, Metadata: string(metadata), ErrorCode: "account_cost_account_not_found",
	}})
	require.NoError(t, err)
	require.Equal(t, 1, artifact.WarningCount)
	path, _, err := accountExportTaskPaths("systask_account_cost_warning")
	require.NoError(t, err)
	workbook, err := excelize.OpenFile(path, excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = workbook.Close() })
	require.Equal(t, "missing-account", accountCostCell(t, workbook, "B2"))
	require.Empty(t, accountCostCell(t, workbook, "H2"))
	require.Equal(t, "失败", accountCostCell(t, workbook, "N2"))
	require.Equal(t, "账号已不存在", accountCostCell(t, workbook, "O2"))
}

func accountCostCell(t *testing.T, workbook *excelize.File, cell string) string {
	t.Helper()
	value, err := workbook.GetCellValue("历史消费", cell, excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	return value
}

func testAccountCostConnector(t *testing.T, baseURL string) *Connector {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	require.NoError(t, err)
	return &Connector{baseURL: parsed, client: http.DefaultClient, maxBodyBytes: accountCostExportMaxBodyBytes}
}
