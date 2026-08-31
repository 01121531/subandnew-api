package billingalert

import (
	"context"
	"encoding/csv"
	"os"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestRunAlertExportCreatesReusableCSV(t *testing.T) {
	setupRepositoryTestDB(t)
	t.Setenv("BILLING_ALERT_EXPORT_DIR", t.TempDir())

	events := []*model.BillingAlertEvent{
		{
			EventKey: "billing-export-1", EventType: "threshold", RuleName: "monthly",
			InstanceName: "=HYPERLINK(\"https://example.invalid\")", InstanceKind: "new-api", ThresholdName: "warning",
			Currency: model.BillingCurrencyCNY, Threshold: "100", USDTotal: "20",
			CNYTotal: "102.4", DiscountRate: "0.8", ExchangeRate: "6.4",
			ExchangeSource: "ecb", ExchangeObservedDate: "2026-08-13",
			Recipients: `["ops@example.com"]`, CreatedAt: 1_786_598_820,
		},
		{
			EventKey: "billing-export-2", EventType: "monitor_recovery", RuleName: "daily",
			InstanceName: "sub2-a", InstanceKind: "sub2api", USDTotal: "5",
			CNYTotal: "32", DiscountRate: "1", ExchangeRate: "6.4",
			Recipients: `["owner@example.com"]`, CreatedAt: 1_786_598_821,
		},
		{
			EventKey: "instance-export-1", EventType: model.InstanceAlertEventFailure,
			SourceType: model.AlertSourceInstance, SourceRecordID: 9, RuleName: "实例巡检",
			InstanceName: "gateway-a", InstanceKind: model.ManagedInstanceKindClaudeGateway,
			ThresholdName: "实例不可用", ErrorCode: "target_timeout",
			Recipients: "ops@example.com", CreatedAt: 1_786_598_822,
		},
	}
	require.NoError(t, model.DB.Create(&events).Error)

	record := &model.BillingAlertExport{
		TaskID: "billing-alert-export-test", ActorID: 1, Query: `{}`, Status: "pending",
	}
	require.NoError(t, model.DB.Create(record).Error)
	require.NoError(t, RunAlertExport(context.Background(), record.ID))

	require.NoError(t, model.DB.First(record, record.ID).Error)
	require.Equal(t, "succeeded", record.Status)
	require.Equal(t, int64(3), record.RecordCount)
	require.NotZero(t, record.FileSize)
	require.NotZero(t, record.ExpiresAt)
	require.FileExists(t, record.FilePath)

	file, err := os.Open(record.FilePath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	rows, err := csv.NewReader(file).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 4)
	require.Equal(t, "来源", rows[0][0])
	require.Equal(t, model.AlertSourceBilling, rows[1][0])
	require.Equal(t, "threshold", rows[1][1])
	require.Equal(t, "'=HYPERLINK(\"https://example.invalid\")", rows[1][2])
	require.Equal(t, "80%", rows[1][14])
	require.Equal(t, "monitor_recovery", rows[2][1])
	require.Equal(t, "100%", rows[2][14])
	require.Equal(t, model.AlertSourceInstance, rows[3][0])
	require.Equal(t, model.InstanceAlertEventFailure, rows[3][1])
	require.Equal(t, "target_timeout", rows[3][19])
}
