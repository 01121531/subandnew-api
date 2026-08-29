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
		&model.ManagedInstance{}, &model.ManagedInstanceCredential{}, &model.ManagedDashboardSnapshot{}, &model.ManagedAccountSnapshot{}, &model.ManagedInstanceAlert{}, &model.ManagedRPMHistory{}, &model.SystemTask{},
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

func TestAssistantToolTimesUseSingleShanghaiRepresentation(t *testing.T) {
	location := time.FixedZone(assistantTimezone, 8*60*60)
	observedAt := time.Date(2026, 8, 29, 1, 5, 58, 0, location).Unix()
	expected := "2026-08-29T01:05:58+08:00"
	require.Equal(t, expected, assistantTime(observedAt))

	encoded, err := json.Marshal(realtimeOutput{Items: []realtimeItem{{InstanceID: 6, ObservedAt: assistantTime(observedAt), Status: "connected"}}})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"observed_at":"`+expected+`"`)
	require.NotContains(t, string(encoded), jsonNumber(observedAt))
}

func TestRuntimeContextReturnsShanghaiTimeAndEffectiveDefault(t *testing.T) {
	db, execution, visible, _ := newBuiltinTestContext(t)
	require.NoError(t, db.Model(&model.AssistantIdentity{}).Where("id = ?", execution.IdentityID).Update("default_instance_id", visible.Id).Error)
	registry, err := NewRegistry(db)
	require.NoError(t, err)
	result, err := registry.Execute(t.Context(), execution, "get_runtime_context", json.RawMessage(`{}`))
	require.NoError(t, err)
	var output runtimeContextOutput
	require.NoError(t, json.Unmarshal(result.Data, &output))
	require.Equal(t, assistantTimezone, output.Timezone)
	require.Equal(t, visible.Id, *output.DefaultInstanceID)
	require.Equal(t, visible.Name, output.DefaultInstance)
	parsed, err := time.Parse(time.RFC3339, output.CurrentTime)
	require.NoError(t, err)
	_, offset := parsed.Zone()
	require.Equal(t, 8*60*60, offset)
}

func TestToolGuideReturnsOnlyRequestedEmbeddedTopic(t *testing.T) {
	db, execution, _, _ := newBuiltinTestContext(t)
	registry, err := NewRegistry(db)
	require.NoError(t, err)
	result, err := registry.Execute(t.Context(), execution, ToolGuide, json.RawMessage(`{"topic":"accounts"}`))
	require.NoError(t, err)
	var output toolGuideOutput
	require.NoError(t, json.Unmarshal(result.Data, &output))
	require.Equal(t, "accounts", output.Topic)
	require.Contains(t, output.Content, "query_managed_accounts")
	require.NotContains(t, output.Content, "get_usage_record_summary")
	require.Equal(t, assistantTimezone, result.Freshness.Timezone)
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
	location := time.FixedZone(assistantTimezone, 8*60*60)
	now := time.Date(2026, 8, 29, 1, 5, 58, 0, location).Unix()
	payload, err := json.Marshal(managedinstance.SummaryResult{
		Window:   managedinstance.TimeWindow{Start: now - 3600, End: now, Timezone: assistantTimezone},
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
	require.Equal(t, tool.FreshnessStale, result.Freshness.State)
	require.Equal(t, now, result.Freshness.ObservedAt.Unix())
	require.Equal(t, assistantTimezone, result.Freshness.Timezone)
	require.Equal(t, assistantTimezone, result.Freshness.ObservedAt.Location().String())
	require.Equal(t, "managed_dashboard_snapshots", result.Provenance[0].Source)
	require.Equal(t, assistantTimezone, result.Provenance[0].ObservedAt.Location().String())
	require.Contains(t, string(result.Data), `"observed_at":"2026-08-29T01:05:58+08:00"`)
	require.Contains(t, string(result.Data), `"end_at":"2026-08-29T01:05:58+08:00"`)
	require.NotContains(t, string(result.Data), jsonNumber(now))
	require.NotContains(t, string(result.Data), `"start":`)
	require.NotContains(t, string(result.Data), `"end":`)
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
	require.Equal(t, "2026-08-28T15:00:00+08:00", output.Points[0].Time)
	require.Equal(t, output.Points[0].Time, output.Statistics["rpm"].LatestAt)
	require.NotContains(t, string(result.Data), `"timestamp"`)
	require.NotContains(t, string(result.Data), jsonNumber(bucket))
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

func TestMetricHistoryTreatsUTCAndShanghaiInputsAsSameInstant(t *testing.T) {
	utcQuery, err := normalizeMetricHistoryQuery(metricHistoryInput{
		Metrics: []string{"rpm"}, Mode: "point", PointAt: "2026-08-28T17:05:58Z", Granularity: "minute",
	})
	require.NoError(t, err)
	shanghaiQuery, err := normalizeMetricHistoryQuery(metricHistoryInput{
		Metrics: []string{"rpm"}, Mode: "point", PointAt: "2026-08-29 01:05:58", Granularity: "minute",
	})
	require.NoError(t, err)
	require.Equal(t, shanghaiQuery, utcQuery)
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
	require.Equal(t, assistantTime(now), health.Items[0].LastCheckedAt)
	require.NotContains(t, string(healthResult.Data), jsonNumber(now))

	alertResult, err := registry.Execute(t.Context(), execution, "get_open_alerts", json.RawMessage(`{}`))
	require.NoError(t, err)
	var alerts alertsOutput
	require.NoError(t, json.Unmarshal(alertResult.Data, &alerts))
	require.Len(t, alerts.Items, 1)
	require.Equal(t, visible.Id, alerts.Items[0].InstanceID)
	require.Equal(t, assistantTime(now), alerts.Items[0].LastSeenAt)
	require.NotContains(t, string(alertResult.Data), jsonNumber(now))
	require.NotContains(t, string(alertResult.Data), "email")
}

func TestManagedAccountQueryUsesSnapshotFiltersAndSanitizesNotes(t *testing.T) {
	db, execution, visible, _ := newBuiltinTestContext(t)
	now := time.Now().Unix()
	available := true
	unavailable := false
	payload, err := json.Marshal(managedinstance.InventoryPage{
		ResourceKind: "channel",
		Sources:      []managedinstance.InventorySource{{ID: "node-1", Name: "worker-a"}},
		Items: []managedinstance.InventoryItem{
			{ID: 101, IDText: "account-101", Name: "Alice", Email: "alice@example.com", Note: "owner https://internal.example password=secret-value", SourceID: "node-1", Enabled: &available, CreatedAt: now - 60},
			{ID: 102, IDText: "account-102", Name: "Bob", Email: "bob@other.test", Enabled: &unavailable, CreatedAt: now - 120},
		},
		Total: 2,
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ManagedAccountSnapshot{
		InstanceID: visible.Id, SnapshotKind: model.ManagedAccountSnapshotKindInventory, RangeKey: "inventory",
		Timezone: assistantTimezone, SchemaVersion: 2, ObservedAt: now, Payload: string(payload),
		LastAttemptAt: now, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}).Error)
	registry, err := NewRegistry(db)
	require.NoError(t, err)
	result, err := registry.Execute(t.Context(), execution, "query_managed_accounts", json.RawMessage(`{
		"instance_ids":[`+jsonNumber(visible.Id)+`],"dataset":"inventory","match_mode":"all",
		"rules":[{"field":"email","operator":"contains","values":["example.com"],"value_mode":"any"}]
	}`))
	require.NoError(t, err)
	var output managedAccountsOutput
	require.NoError(t, json.Unmarshal(result.Data, &output))
	require.Equal(t, 1, output.Total)
	require.Equal(t, "alice@example.com", output.Items[0].Email)
	require.Equal(t, "worker-a", output.Items[0].SourceName)
	require.NotContains(t, output.Items[0].Note, "https://")
	require.NotContains(t, output.Items[0].Note, "secret-value")
	require.Contains(t, output.Items[0].Note, "[已隐藏]")
	require.Equal(t, 1, output.Summary.Available)
	require.Equal(t, tool.FreshnessSnapshot, result.Freshness.State)
	require.Equal(t, assistantTime(now), output.Sources[0].ObservedAt)
}

func TestUsageRecordMapperReturnsBusinessFieldsButDropsRawSensitiveData(t *testing.T) {
	raw := json.RawMessage(`{
		"created_at":"2026-08-29T01:05:58+08:00","user_id":7,"user":{"email":"ops@example.com"},
		"api_key_id":8,"api_key":{"name":"production"},"account_id":6822196335042536377,"account":{"name":"account-a"},
		"model":"gpt-5","request_id":"request-123","input_tokens":10,"output_tokens":20,
		"cache_read_tokens":3,"cache_creation_tokens":4,"actual_cost":1.25,"duration_ms":1200,
		"ip_address":"127.0.0.1","content":"do not expose"
	}`)
	item, err := assistantUsageRecordFromRaw(model.ManagedInstanceKindSub2API, raw)
	require.NoError(t, err)
	require.Equal(t, "ops@example.com", item.User)
	require.Equal(t, "6822196335042536377", item.AccountID)
	require.Equal(t, "request-123", item.RequestID)
	require.Equal(t, 37.0, *item.TotalTokens)
	encoded, err := json.Marshal(item)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "127.0.0.1")
	require.NotContains(t, string(encoded), "do not expose")
}

func TestFailedAccountOutputDoesNotBecomeZeroUsage(t *testing.T) {
	instance := &managedinstance.InstanceView{ManagedInstance: &model.ManagedInstance{Id: 1, Name: "test", Kind: model.ManagedInstanceKindSub2API}}
	row := accountOutputRow(instance, managedinstance.AccountOutputItem{
		Account:          managedinstance.InventoryItem{ID: 10, Name: "account"},
		CollectionStatus: model.ManagedInstanceCollectionFailed, ErrorCode: "timeout",
	}, nil)
	require.Nil(t, row.item.Requests)
	require.Nil(t, row.item.Tokens)
	require.Nil(t, row.item.Amount)
	require.Equal(t, "timeout", row.item.ErrorCode)
}

func TestUsageRecordToolsReturnUnsupportedForClaudeGateway(t *testing.T) {
	db, execution, visible, _ := newBuiltinTestContext(t)
	require.NoError(t, db.Model(&model.ManagedInstance{}).Where("id = ?", visible.Id).Update("kind", model.ManagedInstanceKindClaudeGateway).Error)
	registry, err := NewRegistry(db)
	require.NoError(t, err)
	result, err := registry.Execute(t.Context(), execution, "query_usage_records", json.RawMessage(`{"instance_ids":[`+jsonNumber(visible.Id)+`]}`))
	require.NoError(t, err)
	var output usageRecordsOutput
	require.NoError(t, json.Unmarshal(result.Data, &output))
	require.Equal(t, "unsupported", output.Status)
	require.Equal(t, "unsupported_capability", output.ErrorCode)
	require.Empty(t, output.Items)
}

func TestSafeBusinessTextAllowsEmailAndRedactsConnectionsAndCredentials(t *testing.T) {
	value := safeBusinessText("ops@example.com https://internal.example 127.0.0.1:8080 password=secret-value")
	require.Contains(t, value, "ops@example.com")
	require.NotContains(t, value, "https://")
	require.NotContains(t, value, "127.0.0.1")
	require.NotContains(t, value, "secret-value")
}

func TestAssistantUsageRangeUsesShanghaiTimeOnce(t *testing.T) {
	utcStart, utcEnd, err := normalizeAssistantUsageRange("2026-08-28T17:05:58Z", "2026-08-28T18:05:58Z", 31)
	require.NoError(t, err)
	localStart, localEnd, err := normalizeAssistantUsageRange("2026-08-29 01:05:58", "2026-08-29 02:05:58", 31)
	require.NoError(t, err)
	require.Equal(t, localStart.Unix(), utcStart.Unix())
	require.Equal(t, localEnd.Unix(), utcEnd.Unix())
	require.Equal(t, "2026-08-29T01:05:58+08:00", localStart.Format(time.RFC3339))
}

func TestAssistantUsageFiltersMapOnlyToNativePlatformFields(t *testing.T) {
	start, _, err := parseAssistantHistoryTime("2026-08-29 00:00:00", false)
	require.NoError(t, err)
	end, _, err := parseAssistantHistoryTime("2026-08-29 23:59:59", false)
	require.NoError(t, err)
	newAPIQuery, err := assistantUsageValues(model.ManagedInstanceKindNewAPI, start, end, usageQueryFilters{
		Usernames: []string{"alice"}, Models: []string{"gpt-5"}, Channels: []string{"7"},
	})
	require.NoError(t, err)
	require.Equal(t, "alice", newAPIQuery.Get("username"))
	require.Equal(t, "gpt-5", newAPIQuery.Get("model_name"))
	require.Equal(t, "7", newAPIQuery.Get("channel"))
	require.NotEmpty(t, newAPIQuery.Get("start_timestamp"))

	sub2Query, err := assistantUsageValues(model.ManagedInstanceKindSub2API, start, end, usageQueryFilters{
		UserIDs: []string{"11"}, AccountIDs: []string{"12"}, Models: []string{"claude"},
	})
	require.NoError(t, err)
	require.Equal(t, "11", sub2Query.Get("user_id"))
	require.Equal(t, "12", sub2Query.Get("account_id"))
	require.Equal(t, assistantTimezone, sub2Query.Get("timezone"))

	_, err = assistantUsageValues(model.ManagedInstanceKindConductor, start, end, usageQueryFilters{RequestIDs: []string{"not-supported"}})
	require.Error(t, err)
}

func floatPointer(value float64) *float64 { return &value }

func jsonNumber(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
