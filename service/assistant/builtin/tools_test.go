package builtin

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newBuiltinTestContext(t *testing.T) (*gorm.DB, tool.ExecutionContext, model.ManagedInstance, model.ManagedInstance) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.AssistantIdentity{}, &model.AssistantIdentityInstanceScope{}, &model.AssistantSetting{},
		&model.ManagedInstance{}, &model.ManagedInstanceCredential{}, &model.ManagedDashboardSnapshot{}, &model.ManagedInstanceAlert{}, &model.ManagedRPMHistory{},
	))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	user := model.User{Username: "assistant-root", Password: "hash", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	identity := model.AssistantIdentity{
		ChannelID: 1, ExternalUserID: "wx-user", UserID: user.Id, Status: model.AssistantIdentityStatusActive,
		AllowedInstanceScope: model.AssistantInstanceScopeSelected,
	}
	require.NoError(t, db.Create(&identity).Error)
	visible := model.ManagedInstance{Name: "visible", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://visible.example", Environment: "production", Status: model.ManagedInstanceStatusHealthy}
	hidden := model.ManagedInstance{Name: "hidden", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://hidden.example", Environment: "production", Status: model.ManagedInstanceStatusOffline}
	require.NoError(t, db.Create(&visible).Error)
	require.NoError(t, db.Create(&hidden).Error)
	require.NoError(t, db.Create(&model.AssistantIdentityInstanceScope{IdentityID: identity.ID, InstanceID: visible.Id}).Error)
	execution := tool.ExecutionContext{
		RunID: "run", ConversationID: "conversation", Channel: "wechat", IdentityID: identity.ID, UserID: user.Id, UserRole: user.Role,
	}
	return db, execution, visible, hidden
}

func TestListInstancesReturnsOnlyScopedRedactedData(t *testing.T) {
	db, execution, visible, hidden := newBuiltinTestContext(t)
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	result, err := registry.Execute(t.Context(), execution, "list_instances", json.RawMessage(`{}`))
	require.NoError(t, err)
	var output listInstancesOutput
	require.NoError(t, json.Unmarshal(result.Data, &output))
	require.Equal(t, int64(1), output.Total)
	require.Equal(t, visible.Id, output.Items[0].ID)
	require.NotEqual(t, hidden.Id, output.Items[0].ID)
	require.NotContains(t, string(result.Data), "base_url")
	require.NotContains(t, string(result.Data), "visible.example")
}

func TestListInstancesUsesDefaultAndAllowsExplicitAllScope(t *testing.T) {
	db, execution, visible, hidden := newBuiltinTestContext(t)
	require.NoError(t, db.Create(&model.AssistantIdentityInstanceScope{IdentityID: execution.IdentityID, InstanceID: hidden.Id}).Error)
	require.NoError(t, db.Model(&model.AssistantIdentity{}).Where("id = ?", execution.IdentityID).Update("default_instance_id", visible.Id).Error)
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	result, err := registry.Execute(t.Context(), execution, "list_instances", json.RawMessage(`{}`))
	require.NoError(t, err)
	var defaultOutput listInstancesOutput
	require.NoError(t, json.Unmarshal(result.Data, &defaultOutput))
	require.Equal(t, int64(1), defaultOutput.Total)
	require.Equal(t, visible.Id, defaultOutput.Items[0].ID)

	result, err = registry.Execute(t.Context(), execution, "list_instances", json.RawMessage(`{"instance_scope":"all"}`))
	require.NoError(t, err)
	var allOutput listInstancesOutput
	require.NoError(t, json.Unmarshal(result.Data, &allOutput))
	require.Equal(t, int64(2), allOutput.Total)

	_, err = registry.Execute(t.Context(), execution, "list_instances", json.RawMessage(`{"instance_ids":[1],"instance_scope":"all"}`))
	require.Error(t, err)
}

func TestDashboardSummaryCarriesProvenanceAndFreshness(t *testing.T) {
	db, execution, visible, _ := newBuiltinTestContext(t)
	now := time.Now().Unix()
	payload, err := json.Marshal(managedinstance.SummaryResult{
		Requests: managedinstance.MetricSample{Value: floatPointer(123), Unit: "request", CollectionStatus: model.ManagedInstanceCollectionSucceeded},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ManagedDashboardSnapshot{
		InstanceID: visible.Id, RangeKey: "preset-7", PresetDays: 7, ObservedAt: now, Payload: string(payload),
		LastAttemptAt: now, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	result, err := registry.Execute(t.Context(), execution, "get_dashboard_summary", json.RawMessage(`{"instance_ids":[`+jsonNumber(visible.Id)+`],"preset_days":7}`))
	require.NoError(t, err)
	require.Equal(t, tool.FreshnessSnapshot, result.Freshness.State)
	require.Equal(t, now, result.Freshness.ObservedAt.Unix())
	require.Equal(t, assistantTimezone, result.Freshness.Timezone)
	require.Equal(t, assistantTimezone, result.Freshness.ObservedAt.Location().String())
	require.Equal(t, "managed_dashboard_snapshots", result.Provenance[0].Source)
	require.Equal(t, assistantTimezone, result.Provenance[0].ObservedAt.Location().String())
	require.Contains(t, string(result.Data), `"observed_at":`)
}

func TestRealtimeMetricsDoesNotExposeAccountDetails(t *testing.T) {
	db, execution, visible, _ := newBuiltinTestContext(t)
	registry, err := NewRegistry(db)
	require.NoError(t, err)
	result, err := registry.Execute(t.Context(), execution, "get_realtime_metrics", json.RawMessage(`{"instance_ids":[`+jsonNumber(visible.Id)+`]}`))
	require.NoError(t, err)
	require.Equal(t, tool.FreshnessUnknown, result.Freshness.State)
	require.NotContains(t, string(result.Data), `"accounts"`)
}

func TestMetricHistoryUsesDefaultInstanceAndShanghaiTime(t *testing.T) {
	db, execution, visible, _ := newBuiltinTestContext(t)
	require.NoError(t, db.Model(&model.AssistantIdentity{}).Where("id = ?", execution.IdentityID).Update("default_instance_id", visible.Id).Error)
	location := time.FixedZone(assistantTimezone, 8*60*60)
	bucket := time.Date(2026, 8, 28, 15, 0, 0, 0, location).Unix()
	require.NoError(t, db.Create(&model.ManagedRPMHistory{
		InstanceID: visible.Id, BucketStart: bucket, RPMSum: 92, RPMLast: 50, SampleCount: 2,
	}).Error)
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	result, err := registry.Execute(t.Context(), execution, "get_metric_history", json.RawMessage(`{
		"metrics":["rpm","accounts_available"],"mode":"point","point_at":"2026-08-28 15:00:10","granularity":"minute"
	}`))
	require.NoError(t, err)
	var output metricHistoryOutput
	require.NoError(t, json.Unmarshal(result.Data, &output))
	require.Equal(t, []int64{visible.Id}, output.InstanceIDs)
	require.Equal(t, assistantTimezone, output.Timezone)
	require.Len(t, output.Points, 1)
	require.Equal(t, 50.0, *output.Points[0].Values["rpm"])
	require.Equal(t, "unsupported", output.MetricStatus["accounts_available"].Status)
	require.Equal(t, "2026-08-28 15:00:00", output.Points[0].Time)
}

func TestMetricHistoryReadsAuxiliarySampleWithoutRPM(t *testing.T) {
	db, execution, visible, _ := newBuiltinTestContext(t)
	require.NoError(t, db.Model(&visible).Update("kind", model.ManagedInstanceKindSub2API).Error)
	location := time.FixedZone(assistantTimezone, 8*60*60)
	bucket := time.Date(2026, 8, 28, 16, 0, 0, 0, location).Unix()
	require.NoError(t, db.Create(&model.ManagedRPMHistory{
		InstanceID: visible.Id, BucketStart: bucket, ConcurrencyUsedLast: 0, ConcurrencyMaxLast: 400,
		ConcurrencySampleCount: 1, ConcurrencyUsedSamples: 1, ConcurrencyMaxSamples: 1,
	}).Error)
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	result, err := registry.Execute(t.Context(), execution, "get_metric_history", json.RawMessage(`{
		"instance_ids":[`+jsonNumber(visible.Id)+`],"metrics":["concurrency_used","concurrency_max"],
		"mode":"point","point_at":"2026-08-28 16:00:30","granularity":"minute"
	}`))
	require.NoError(t, err)
	var output metricHistoryOutput
	require.NoError(t, json.Unmarshal(result.Data, &output))
	require.Len(t, output.Points, 1)
	require.NotNil(t, output.Points[0].Values["concurrency_used"])
	require.Zero(t, *output.Points[0].Values["concurrency_used"])
	require.Equal(t, 400.0, *output.Points[0].Values["concurrency_max"])
}

func TestMetricHistoryReadsDailyDashboardTrend(t *testing.T) {
	db, execution, visible, _ := newBuiltinTestContext(t)
	now := time.Now().Unix()
	payload, err := json.Marshal(managedinstance.SummaryResult{Trend: []managedinstance.UsageTrendPoint{
		{Date: "2026-08-27", Requests: 12, Tokens: 34, Cost: 5.6789},
		{Date: "2026-08-28", Requests: 56, Tokens: 78, Cost: 9.0123},
	}})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ManagedDashboardSnapshot{
		InstanceID: visible.Id, RangeKey: "preset-30", PresetDays: 30, ObservedAt: now, Payload: string(payload),
		LastAttemptAt: now, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	result, err := registry.Execute(t.Context(), execution, "get_metric_history", json.RawMessage(`{
		"instance_ids":[`+jsonNumber(visible.Id)+`],"metrics":["requests","tokens","actual_cost"],
		"mode":"series","start_at":"2026-08-27","end_at":"2026-08-28","granularity":"day"
	}`))
	require.NoError(t, err)
	var output metricHistoryOutput
	require.NoError(t, json.Unmarshal(result.Data, &output))
	require.Len(t, output.Points, 2)
	require.Equal(t, 68.0, *output.Statistics["requests"].Sum)
	require.Equal(t, 9.0123, *output.Statistics["actual_cost"].Latest)
}

func TestMetricHistoryRejectsOverlongAndOversizedRanges(t *testing.T) {
	_, err := normalizeMetricHistoryQuery(metricHistoryInput{
		Metrics: []string{"rpm"}, Mode: "series", StartAt: "2026-07-01", EndAt: "2026-08-28", Granularity: "day",
	})
	require.Error(t, err)
	_, err = normalizeMetricHistoryQuery(metricHistoryInput{
		Metrics: []string{"rpm"}, Mode: "series", StartAt: "2026-08-27", EndAt: "2026-08-28", Granularity: "minute",
	})
	require.Error(t, err)
}

func TestHealthAndAlertsStayWithinIdentityScope(t *testing.T) {
	db, execution, visible, hidden := newBuiltinTestContext(t)
	now := time.Now().Unix()
	require.NoError(t, db.Model(&visible).Updates(map[string]any{"consecutive_failures": 2, "last_checked_at": now}).Error)
	require.NoError(t, db.Create(&model.ManagedInstanceAlert{InstanceId: visible.Id, AlertType: model.ManagedInstanceAlertTypeAvailability, ErrorCode: "timeout", LastSeenAt: now}).Error)
	require.NoError(t, db.Create(&model.ManagedInstanceAlert{InstanceId: hidden.Id, AlertType: model.ManagedInstanceAlertTypeCredential, ErrorCode: "auth_failed", LastSeenAt: now}).Error)
	registry, err := NewRegistry(db)
	require.NoError(t, err)

	healthResult, err := registry.Execute(t.Context(), execution, "get_instance_health", json.RawMessage(`{}`))
	require.NoError(t, err)
	var health healthOutput
	require.NoError(t, json.Unmarshal(healthResult.Data, &health))
	require.Len(t, health.Items, 1)
	require.Equal(t, visible.Id, health.Items[0].InstanceID)
	require.Equal(t, 2, health.Items[0].ConsecutiveFailures)

	alertResult, err := registry.Execute(t.Context(), execution, "get_open_alerts", json.RawMessage(`{}`))
	require.NoError(t, err)
	var alerts alertsOutput
	require.NoError(t, json.Unmarshal(alertResult.Data, &alerts))
	require.Len(t, alerts.Items, 1)
	require.Equal(t, visible.Id, alerts.Items[0].InstanceID)
	require.NotContains(t, string(alertResult.Data), "email")
}

func floatPointer(value float64) *float64 { return &value }

func jsonNumber(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
