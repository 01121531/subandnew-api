package billingalert

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
)

func TestEvaluateRuleInstanceCreatesThresholdEventOncePerCycle(t *testing.T) {
	setupRepositoryTestDB(t)
	rule, instance := createEvaluationFixture(t, "10")
	rate, err := SaveExchangeRate(RateQuote{
		Base: "USD", Quote: "CNY", Rate: "7.2", ObservedDate: "2026-08-13", Source: ExchangeSourceECB,
	})
	require.NoError(t, err)
	require.NotZero(t, rate.ID)

	previous := usageSummaryFetcher
	usageSummaryFetcher = func(_ context.Context, id int64, query url.Values) (*managedinstance.UsageRecordSummary, error) {
		require.Equal(t, instance.Id, id)
		require.Equal(t, "alice", query.Get("username"))
		return &managedinstance.UsageRecordSummary{
			SourceInstanceID: id, Kind: model.ManagedInstanceKindNewAPI,
			Amount: 12.5, Currency: model.BillingCurrencyUSD,
		}, nil
	}
	t.Cleanup(func() { usageSummaryFetcher = previous })
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))

	first, err := EvaluateRuleInstance(context.Background(), rule.ID, instance.Id, now)
	require.NoError(t, err)
	require.Equal(t, "12.50000000", first.USDTotal)
	require.Equal(t, "72.00000000", first.CNYTotal)
	require.Len(t, first.Triggered, 1)

	second, err := EvaluateRuleInstance(context.Background(), rule.ID, instance.Id, now.Add(time.Minute))
	require.NoError(t, err)
	require.Empty(t, second.Triggered)
	var eventCount int64
	require.NoError(t, model.DB.Model(&model.BillingAlertEvent{}).Where("event_type = ?", model.BillingAlertEventThreshold).Count(&eventCount).Error)
	require.Equal(t, int64(1), eventCount)
	var deliveryCount int64
	require.NoError(t, model.DB.Model(&model.BillingEmailDelivery{}).Count(&deliveryCount).Error)
	require.Equal(t, int64(1), deliveryCount)
	var event model.BillingAlertEvent
	require.NoError(t, model.DB.Where("event_type = ?", model.BillingAlertEventThreshold).First(&event).Error)
	require.Equal(t, rule.Name, event.RuleName)
	require.Equal(t, instance.Name, event.InstanceName)
	require.Equal(t, "alice", parseEventFilter(t, event.Filters, "username"))
}

func TestUnsupportedFilterDoesNotFallBackToAllRecords(t *testing.T) {
	setupRepositoryTestDB(t)
	rule, instance := createEvaluationFixture(t, "10")
	_, err := SaveExchangeRate(RateQuote{Base: "USD", Quote: "CNY", Rate: "7", ObservedDate: "2026-08-13", Source: ExchangeSourceECB})
	require.NoError(t, err)
	var template model.BillingFilterTemplateVersion
	require.NoError(t, model.DB.Where("template_id = ?", rule.TemplateID).First(&template).Error)
	require.NoError(t, model.DB.Model(&template).Update("filters", `{"account_id":["99"]}`).Error)
	previous := usageSummaryFetcher
	usageSummaryFetcher = func(context.Context, int64, url.Values) (*managedinstance.UsageRecordSummary, error) {
		t.Fatal("unsupported filters must not request an unfiltered total")
		return nil, nil
	}
	t.Cleanup(func() { usageSummaryFetcher = previous })
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	result, err := EvaluateRuleInstance(context.Background(), rule.ID, instance.Id, now)
	require.NoError(t, err)
	require.Equal(t, "0.00000000", result.USDTotal)
	require.Empty(t, result.Triggered)
	var binding model.BillingAlertRuleInstance
	require.NoError(t, model.DB.Where("rule_id = ? AND instance_id = ?", rule.ID, instance.Id).First(&binding).Error)
	require.Equal(t, "unmatched:account_id", binding.FilterStatus)
}

func TestEvaluationFailureAndRecoveryNotifications(t *testing.T) {
	setupRepositoryTestDB(t)
	rule, instance := createEvaluationFixture(t, "1000")
	_, err := SaveExchangeRate(RateQuote{
		Base: "USD", Quote: "CNY", Rate: "7", ObservedDate: "2026-08-13", Source: ExchangeSourceECB,
	})
	require.NoError(t, err)
	previous := usageSummaryFetcher
	failing := true
	usageSummaryFetcher = func(_ context.Context, id int64, _ url.Values) (*managedinstance.UsageRecordSummary, error) {
		if failing {
			return nil, errors.New("upstream failed")
		}
		return &managedinstance.UsageRecordSummary{SourceInstanceID: id, Amount: 1, Currency: model.BillingCurrencyUSD}, nil
	}
	t.Cleanup(func() { usageSummaryFetcher = previous })
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	for index := 0; index < 3; index++ {
		_, err := EvaluateRuleInstance(context.Background(), rule.ID, instance.Id, now.Add(time.Duration(index)*time.Minute))
		require.Error(t, err)
	}
	var failures int64
	require.NoError(t, model.DB.Model(&model.BillingAlertEvent{}).Where("event_type = ?", model.BillingAlertEventFailure).Count(&failures).Error)
	require.Equal(t, int64(1), failures)

	failing = false
	_, err = EvaluateRuleInstance(context.Background(), rule.ID, instance.Id, now.Add(4*time.Minute))
	require.NoError(t, err)
	var recoveries int64
	require.NoError(t, model.DB.Model(&model.BillingAlertEvent{}).Where("event_type = ?", model.BillingAlertEventRecovery).Count(&recoveries).Error)
	require.Equal(t, int64(1), recoveries)
}

func createEvaluationFixture(t *testing.T, thresholdAmount string) (*RuleView, *model.ManagedInstance) {
	t.Helper()
	instance := &model.ManagedInstance{Name: "evaluation-instance-" + thresholdAmount, Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://evaluation-" + thresholdAmount + ".example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	template, err := CreateTemplate(TemplateInput{Name: "evaluation-template-" + thresholdAmount, Filters: map[string][]string{"username": {"alice"}}}, 1)
	require.NoError(t, err)
	rule, err := CreateRule(RuleInput{
		Name: "evaluation-rule-" + thresholdAmount, TemplateID: template.ID, Enabled: true,
		Timezone: "Asia/Shanghai", CycleType: CycleNaturalDay, CycleConfig: json.RawMessage(`{}`),
		DiscountRate: "0.8", ExchangeMode: ExchangeModeLatest, ExchangeOverride: true,
		ScheduleType: ScheduleInterval, ScheduleConfig: json.RawMessage(`{"seconds":300}`),
		Recipients: []string{"ops@example.com"}, FailureThreshold: 3, InstanceIDs: []int64{instance.Id},
		Thresholds: []ThresholdInput{{
			Name: "warning", Severity: "warning", Currency: model.BillingCurrencyUSD,
			Amount: thresholdAmount, ReminderMode: ReminderOnce,
		}},
	}, 1)
	require.NoError(t, err)
	return rule, instance
}

func parseEventFilter(t *testing.T, encoded string, key string) string {
	t.Helper()
	var filters map[string][]string
	require.NoError(t, json.Unmarshal([]byte(encoded), &filters))
	require.NotEmpty(t, filters[key])
	return filters[key][0]
}
