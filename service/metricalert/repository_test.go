package metricalert

import (
	"fmt"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMetricAlertTestDB(t *testing.T) {
	t.Helper()
	previous := model.DB
	dsn := fmt.Sprintf("file:metric-alert-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ManagedInstance{},
		&model.MetricAlertRule{},
		&model.MetricAlertRuleInstance{},
		&model.MetricAlertCondition{},
		&model.MetricAlertState{},
		&model.BillingAlertEvent{},
		&model.BillingEmailDelivery{},
	))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, sqlErr := db.DB()
		if sqlErr == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestMetricRuleLifecycleAndCapabilities(t *testing.T) {
	setupMetricAlertTestDB(t)
	conductor := &model.ManagedInstance{Name: "conductor", Kind: model.ManagedInstanceKindConductor, BaseURL: "https://conductor.example.com"}
	newAPI := &model.ManagedInstance{Name: "new-api", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://new-api.example.com"}
	require.NoError(t, model.DB.Create(conductor).Error)
	require.NoError(t, model.DB.Create(newAPI).Error)

	capabilities, err := Capabilities([]int64{conductor.Id, newAPI.Id}, ScopeAggregate)
	require.NoError(t, err)
	keys := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		keys = append(keys, capability.Key)
	}
	require.Contains(t, keys, "rpm")
	require.Contains(t, keys, "today_cost")
	require.NotContains(t, keys, "accounts_available")
	require.NotContains(t, keys, "instance_connected")

	rule, err := CreateRule(RuleInput{
		Name: "low-rpm", Enabled: true, ScopeMode: ScopePerInstance, MatchMode: MatchAll,
		EvaluationIntervalSeconds: 60, TriggerCount: 3, RecoveryCount: 2, FailureThreshold: 3,
		ReminderMode: ReminderOnce, Recipients: []string{"Ops@example.com", "ops@example.com"},
		InstanceIDs: []int64{conductor.Id}, Conditions: []ConditionInput{{Metric: "rpm", Operator: "lt", Threshold: "100", RecoveryThreshold: "120"}},
	}, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"ops@example.com"}, rule.Recipients)
	require.Equal(t, []int64{conductor.Id}, rule.InstanceIDs)
	require.Len(t, rule.Conditions, 1)

	updated, err := UpdateRule(rule.ID, RuleInput{
		Name: "low-rpm-updated", Enabled: true, ScopeMode: ScopePerInstance, MatchMode: MatchAny,
		EvaluationIntervalSeconds: 30, TriggerCount: 2, RecoveryCount: 1, FailureThreshold: 2,
		ReminderMode: ReminderInterval, RepeatIntervalSeconds: 600, Recipients: []string{"alerts@example.com"},
		InstanceIDs: []int64{conductor.Id}, Conditions: []ConditionInput{{Metric: "rpm_capacity", Operator: "lt", Threshold: "400"}},
	}, 1)
	require.NoError(t, err)
	require.Equal(t, "low-rpm-updated", updated.Name)
	require.Equal(t, int64(30), updated.EvaluationIntervalSeconds)
	require.NoError(t, DeleteRule(rule.ID))
	_, err = GetRule(rule.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMetricAlertConsecutiveTriggerAndRecovery(t *testing.T) {
	setupMetricAlertTestDB(t)
	instance := &model.ManagedInstance{Name: "conductor", Kind: model.ManagedInstanceKindConductor, BaseURL: "https://conductor.example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	rule, err := CreateRule(RuleInput{
		Name: "available-accounts", Enabled: true, ScopeMode: ScopePerInstance, MatchMode: MatchAll,
		EvaluationIntervalSeconds: 60, TriggerCount: 3, RecoveryCount: 2, FailureThreshold: 3,
		ReminderMode: ReminderOnce, Recipients: []string{"ops@example.com"}, InstanceIDs: []int64{instance.Id},
		Conditions: []ConditionInput{{Metric: "accounts_available", Operator: "lt", Threshold: "10", RecoveryThreshold: "12"}},
	}, 1)
	require.NoError(t, err)

	low := scopeEvaluation{Key: fmt.Sprint(instance.Id), InstanceID: instance.Id, InstanceName: instance.Name, InstanceKind: instance.Kind, Values: map[string]float64{"accounts_available": 8}, ObservedAt: time.Now().Unix()}
	for index := 0; index < 2; index++ {
		triggered, recovered, evalErr := evaluateScope(rule, low)
		require.NoError(t, evalErr)
		require.False(t, triggered)
		require.False(t, recovered)
	}
	triggered, recovered, err := evaluateScope(rule, low)
	require.NoError(t, err)
	require.True(t, triggered)
	require.False(t, recovered)

	high := low
	high.Values = map[string]float64{"accounts_available": 12}
	triggered, recovered, err = evaluateScope(rule, high)
	require.NoError(t, err)
	require.False(t, triggered)
	require.False(t, recovered)
	triggered, recovered, err = evaluateScope(rule, high)
	require.NoError(t, err)
	require.False(t, triggered)
	require.True(t, recovered)

	var events []model.BillingAlertEvent
	require.NoError(t, model.DB.Order("id ASC").Find(&events).Error)
	require.Len(t, events, 2)
	require.Equal(t, model.MetricAlertEventTriggered, events[0].EventType)
	require.Equal(t, model.MetricAlertEventRecovered, events[1].EventType)
	require.Equal(t, model.AlertSourceMetric, events[0].SourceType)
}

func TestMetricAlertDataFailureAndRecovery(t *testing.T) {
	setupMetricAlertTestDB(t)
	instance := &model.ManagedInstance{Name: "sub2", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://sub2.example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	rule, err := CreateRule(RuleInput{
		Name: "sub2-rpm", Enabled: true, ScopeMode: ScopePerInstance, MatchMode: MatchAll,
		EvaluationIntervalSeconds: 60, TriggerCount: 1, RecoveryCount: 1, FailureThreshold: 2,
		ReminderMode: ReminderOnce, Recipients: []string{"ops@example.com"}, InstanceIDs: []int64{instance.Id},
		Conditions: []ConditionInput{{Metric: "rpm", Operator: "lt", Threshold: "10"}},
	}, 1)
	require.NoError(t, err)

	failure := scopeEvaluation{Key: fmt.Sprint(instance.Id), InstanceID: instance.Id, InstanceName: instance.Name, InstanceKind: instance.Kind, Values: map[string]float64{}, ErrorCode: "metric_data_unavailable"}
	_, _, err = evaluateScope(rule, failure)
	require.ErrorIs(t, err, ErrDataUnavailable)
	_, _, err = evaluateScope(rule, failure)
	require.ErrorIs(t, err, ErrDataUnavailable)

	recoveredScope := failure
	recoveredScope.ErrorCode = ""
	recoveredScope.Values = map[string]float64{"rpm": 20}
	recoveredScope.ObservedAt = time.Now().Unix()
	_, _, err = evaluateScope(rule, recoveredScope)
	require.NoError(t, err)

	var events []model.BillingAlertEvent
	require.NoError(t, model.DB.Order("id ASC").Find(&events).Error)
	require.Len(t, events, 2)
	require.Equal(t, model.MetricAlertEventMonitorFailure, events[0].EventType)
	require.Equal(t, model.MetricAlertEventMonitorRecovery, events[1].EventType)
}
