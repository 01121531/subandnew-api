package billingalert

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRepositoryTestDB(t *testing.T) {
	t.Helper()
	previous := model.DB
	dsn := fmt.Sprintf("file:billing-repository-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ManagedInstance{},
		&model.BillingFilterTemplate{},
		&model.BillingFilterTemplateVersion{},
		&model.BillingAlertRule{},
		&model.BillingAlertRuleInstance{},
		&model.BillingAlertThreshold{},
		&model.BillingCycleSnapshot{},
		&model.BillingEvaluationSnapshot{},
		&model.BillingAlertEvent{},
		&model.BillingEmailDelivery{},
		&model.ExchangeRate{},
		&model.ExchangeRateSetting{},
		&model.SMTPSetting{},
		&model.BillingAlertExport{},
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

func TestTemplateAndRuleRepositoryLifecycle(t *testing.T) {
	setupRepositoryTestDB(t)
	instance := &model.ManagedInstance{Name: "billing-new-api", Kind: "new-api", BaseURL: "https://example.com"}
	require.NoError(t, model.DB.Create(instance).Error)

	template, err := CreateTemplate(TemplateInput{
		Name: "production-users",
		Filters: map[string][]string{
			"username": {"alice", "bob"},
			"model":    {"gpt-5"},
		},
	}, 1)
	require.NoError(t, err)
	require.Equal(t, 1, template.CurrentVersion)

	rule, err := CreateRule(RuleInput{
		Name:         "monthly-spend",
		TemplateID:   template.ID,
		Enabled:      true,
		Timezone:     "Asia/Shanghai",
		CycleType:    CycleNaturalMonth,
		CycleConfig:  json.RawMessage(`{}`),
		DiscountRate: "0.8",
		ExchangeMode: ExchangeModeLatest,
		ScheduleType: ScheduleInterval,
		ScheduleConfig: json.RawMessage(`{
			"seconds": 300
		}`),
		Recipients:  []string{"Ops@example.com", "ops@example.com"},
		InstanceIDs: []int64{instance.Id},
		Thresholds: []ThresholdInput{{
			Name: "notice", Severity: "warning", Currency: model.BillingCurrencyCNY,
			Amount: "1000", ReminderMode: ReminderOnce,
		}},
	}, 1)
	require.NoError(t, err)
	require.Len(t, rule.Recipients, 1)
	require.Equal(t, "ops@example.com", rule.Recipients[0])
	require.Equal(t, []int64{instance.Id}, rule.InstanceIDs)
	require.Len(t, rule.Thresholds, 1)

	impact, err := PreviewTemplateUpdate(template.ID, TemplateInput{Filters: map[string][]string{
		"username": {"alice"},
		"channel":  {"primary"},
	}})
	require.NoError(t, err)
	require.Equal(t, int64(1), impact.RuleCount)
	require.Equal(t, []string{"channel"}, impact.AddedFields)
	require.Equal(t, []string{"model"}, impact.RemovedFields)
	require.Equal(t, []string{"username"}, impact.ChangedFields)

	_, err = UpdateTemplate(template.ID, TemplateInput{
		Name: "production-users", Filters: map[string][]string{"username": {"alice"}},
	}, 1)
	require.NoError(t, err)
	updated, err := GetTemplate(template.ID)
	require.NoError(t, err)
	require.Equal(t, 2, updated.CurrentVersion)

	require.Error(t, DeleteTemplate(template.ID))
	require.NoError(t, DeleteRule(rule.ID))
	require.NoError(t, DeleteTemplate(template.ID))
}

func TestCreateRuleRejectsInvalidFixedCycle(t *testing.T) {
	setupRepositoryTestDB(t)
	instance := &model.ManagedInstance{Name: "invalid-cycle-instance", Kind: "new-api", BaseURL: "https://invalid.example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	template, err := CreateTemplate(TemplateInput{Name: "all", Filters: map[string][]string{}}, 1)
	require.NoError(t, err)

	_, err = CreateRule(RuleInput{
		Name: "invalid-fixed", TemplateID: template.ID, Enabled: true,
		CycleType: CycleFixed, CycleConfig: json.RawMessage(`{"start":20,"end":10}`),
		DiscountRate: "1", ExchangeMode: ExchangeModeLatest,
		ScheduleType: ScheduleInterval, ScheduleConfig: json.RawMessage(`{"seconds":300}`),
		Recipients: []string{"ops@example.com"}, InstanceIDs: []int64{instance.Id},
		Thresholds: []ThresholdInput{{
			Name: "notice", Severity: "warning", Currency: model.BillingCurrencyUSD,
			Amount: "10", ReminderMode: ReminderOnce,
		}},
	}, 1)
	require.Error(t, err)
}
