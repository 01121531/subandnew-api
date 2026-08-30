package managedinstance

import (
	"errors"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestAlertRuleConflictAndDisabledRuleReleasesInstances(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	first := &model.ManagedInstance{Name: "first", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://first.example.com"}
	second := &model.ManagedInstance{Name: "second", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://second.example.com"}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)

	rule, err := CreateAlertRule(AlertRuleInput{Name: "primary", Enabled: true,
		AlertTypes: []string{model.ManagedInstanceAlertTypeAvailability}, CheckIntervalSeconds: 120,
		FailureThreshold: 2, InstanceIDs: []int64{first.Id, second.Id}}, 1)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{first.Id, second.Id}, rule.InstanceIDs)
	require.NoError(t, db.First(first, first.Id).Error)
	require.Equal(t, 120, first.CheckIntervalSeconds)

	_, err = CreateAlertRule(AlertRuleInput{Name: "conflict", Enabled: true,
		AlertTypes: []string{model.ManagedInstanceAlertTypeCredential}, CheckIntervalSeconds: 60,
		InstanceIDs: []int64{second.Id}}, 1)
	var conflict *AlertRuleConflictError
	require.True(t, errors.As(err, &conflict))
	require.Equal(t, []int64{second.Id}, conflict.InstanceIDs)

	rule, err = UpdateAlertRule(rule.ID, AlertRuleInput{Name: rule.Name, Enabled: false,
		AlertTypes: rule.AlertTypes, CheckIntervalSeconds: rule.CheckIntervalSeconds,
		FailureThreshold: rule.FailureThreshold, InstanceIDs: rule.InstanceIDs}, 1)
	require.NoError(t, err)
	require.False(t, rule.Enabled)

	other, err := CreateAlertRule(AlertRuleInput{Name: "replacement", Enabled: true,
		AlertTypes: []string{model.ManagedInstanceAlertTypeCredential}, CheckIntervalSeconds: 30,
		InstanceIDs: []int64{second.Id}}, 1)
	require.NoError(t, err)
	require.NoError(t, DeleteAlertRule(other.ID))
}

func TestMigrateLegacyAlertRulesIsIdempotentAndBindsOpenAlerts(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	require.NoError(t, db.Create(&model.SMTPSetting{ID: 1, Enabled: true, AlertRecipients: "ops@example.com", InstanceAlertFailureThreshold: 4}).Error)
	instance := &model.ManagedInstance{Name: "legacy", Kind: model.ManagedInstanceKindConductor,
		BaseURL: "https://legacy.example.com", CheckIntervalSeconds: 90, AlertFailureThreshold: 0}
	require.NoError(t, db.Create(instance).Error)
	require.NoError(t, db.Model(instance).Update("alert_rule_migrated_at", 0).Error)
	alert := &model.ManagedInstanceAlert{InstanceId: instance.Id, AlertType: model.ManagedInstanceAlertTypeAvailability,
		Status: model.ManagedInstanceAlertStatusOpen, ErrorCode: "upstream_failed"}
	require.NoError(t, db.Create(alert).Error)

	first, err := MigrateLegacyAlertRules()
	require.NoError(t, err)
	require.Equal(t, 1, first.Rules)
	require.Equal(t, 1, first.Instances)
	require.NoError(t, db.First(alert, alert.Id).Error)
	require.NotZero(t, alert.RuleID)
	require.Equal(t, "ops@example.com", alert.EmailRecipients)

	second, err := MigrateLegacyAlertRules()
	require.NoError(t, err)
	require.Zero(t, second.Rules)
	require.Zero(t, second.Instances)
}
