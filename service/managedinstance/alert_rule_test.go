package managedinstance

import (
	"errors"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
)

func TestInstanceAlertDataScopeBeforePagination(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AdminDataPolicy{}))
	user := &model.User{Username: "alert-scope", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	p := model.FullAdminDataPolicy()
	p.UserID, p.InstanceScope, p.InstanceIDs = user.Id, "selected", []int64{1}
	require.NoError(t, db.Create(&p).Error)
	a, err := authz.LoadDataAccess(db, user.Id)
	require.NoError(t, err)
	require.NoError(t, db.Create(&[]model.ManagedInstanceAlert{
		{InstanceId: 1, LastSeenAt: 1}, {InstanceId: 2, LastSeenAt: 3}, {InstanceId: 1, LastSeenAt: 2},
	}).Error)
	page, err := ListAlerts(AlertListFilter{Access: a, PageSize: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Items, 1)
	require.EqualValues(t, 2, page.Items[0].LastSeenAt)
	_, err = ListAlerts(AlertListFilter{Access: a, InstanceID: 2})
	require.ErrorIs(t, err, authz.ErrDataForbidden)
	require.NoError(t, db.Create(&model.ManagedInstanceAlertRule{ID: 1, Name: "mixed-scope"}).Error)
	require.NoError(t, db.Create(&[]model.ManagedInstanceAlertRuleInstance{{RuleID: 1, InstanceID: 1}, {RuleID: 1, InstanceID: 2}}).Error)
	rules, err := ListAlertRules(a)
	require.NoError(t, err)
	require.Empty(t, rules)
	_, err = GetAlertRule(1, a)
	require.ErrorIs(t, err, authz.ErrDataForbidden)
	a.Policy.InstanceIDs = nil
	page, err = ListAlerts(AlertListFilter{Access: a})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	require.Empty(t, page.Items)
}

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
