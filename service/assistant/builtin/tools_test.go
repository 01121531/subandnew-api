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
		&model.ManagedInstance{}, &model.ManagedInstanceCredential{}, &model.ManagedDashboardSnapshot{}, &model.ManagedInstanceAlert{},
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
