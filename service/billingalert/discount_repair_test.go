package billingalert

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestRepairLegacyDiscountRatesCorrectsSnapshotsAndEvents(t *testing.T) {
	setupRepositoryTestDB(t)
	rule := &model.BillingAlertRule{
		Name: "legacy-discount", DiscountRate: "55", Recipients: `[]`,
	}
	require.NoError(t, model.DB.Create(rule).Error)
	threshold := &model.BillingAlertThreshold{
		RuleID: rule.ID, Name: "cny-warning", Severity: "warning",
		Currency: model.BillingCurrencyCNY, Amount: "1000", ReminderMode: ReminderOnce,
	}
	require.NoError(t, model.DB.Create(threshold).Error)
	reachedThreshold := &model.BillingAlertThreshold{
		RuleID: rule.ID, Name: "cny-reached", Severity: "warning",
		Currency: model.BillingCurrencyCNY, Amount: "100", ReminderMode: ReminderOnce,
	}
	require.NoError(t, model.DB.Create(reachedThreshold).Error)
	state, err := json.Marshal(map[string]thresholdNotificationState{
		strconv.FormatInt(threshold.ID, 10): {
			LastNotifiedAt: 100, LastAmount: "38500.00000000", Sequence: 1,
		},
		strconv.FormatInt(reachedThreshold.ID, 10): {
			LastNotifiedAt: 100, LastAmount: "38500.00000000", Sequence: 1,
		},
	})
	require.NoError(t, err)
	cycle := &model.BillingCycleSnapshot{
		RuleID: rule.ID, InstanceID: 1, CycleKey: "2026-08", Timezone: "Asia/Shanghai",
		Filters: `{}`, DiscountRate: "55", ExchangeMode: ExchangeModeLatest,
		ThresholdState: string(state),
	}
	require.NoError(t, model.DB.Create(cycle).Error)
	evaluation := &model.BillingEvaluationSnapshot{
		CycleID: cycle.ID, RuleID: rule.ID, InstanceID: 1,
		USDTotal: "100", CNYTotal: "38500.00000000", DiscountRate: "55",
		ExchangeRate: "7", DataTimestamp: 100,
	}
	require.NoError(t, model.DB.Create(evaluation).Error)
	event := &model.BillingAlertEvent{
		EventKey: "legacy-discount-event", EventType: model.BillingAlertEventThreshold,
		RuleID: rule.ID, InstanceID: 1, CycleID: cycle.ID, ThresholdID: threshold.ID,
		USDTotal: "100", CNYTotal: "38500.00000000", DiscountRate: "55",
		ExchangeRate: "7", Recipients: `[]`,
	}
	require.NoError(t, model.DB.Create(event).Error)
	monitorEvent := &model.BillingAlertEvent{
		EventKey: "legacy-discount-monitor", EventType: model.BillingAlertEventRecovery,
		RuleID: rule.ID, InstanceID: 1, Recipients: `[]`,
	}
	require.NoError(t, model.DB.Create(monitorEvent).Error)
	delivery := &model.BillingEmailDelivery{
		EventID: event.ID, Recipient: "ops@example.com", Status: "sent",
	}
	require.NoError(t, model.DB.Create(delivery).Error)

	result, err := RepairLegacyDiscountRates()
	require.NoError(t, err)
	require.Equal(t, 4, result.CorrectedTotal())
	require.Zero(t, result.Invalid)

	require.NoError(t, model.DB.First(rule, rule.ID).Error)
	require.Equal(t, "0.55", rule.DiscountRate)
	require.NoError(t, model.DB.First(evaluation, evaluation.ID).Error)
	require.Equal(t, "0.55", evaluation.DiscountRate)
	require.Equal(t, "385.00000000", evaluation.CNYTotal)
	require.NoError(t, model.DB.First(event, event.ID).Error)
	require.Equal(t, "0.55", event.DiscountRate)
	require.Equal(t, "385.00000000", event.CNYTotal)
	require.NoError(t, model.DB.First(cycle, cycle.ID).Error)
	require.Equal(t, "0.55", cycle.DiscountRate)

	correctedState := map[string]thresholdNotificationState{}
	require.NoError(t, json.Unmarshal([]byte(cycle.ThresholdState), &correctedState))
	thresholdState := correctedState[strconv.FormatInt(threshold.ID, 10)]
	require.Zero(t, thresholdState.LastNotifiedAt)
	require.Empty(t, thresholdState.LastAmount)
	require.Equal(t, 1, thresholdState.Sequence)
	reachedState := correctedState[strconv.FormatInt(reachedThreshold.ID, 10)]
	require.Equal(t, int64(100), reachedState.LastNotifiedAt)
	require.Equal(t, "385.00000000", reachedState.LastAmount)
	require.Equal(t, 1, reachedState.Sequence)

	var deliveries int64
	require.NoError(t, model.DB.Model(&model.BillingEmailDelivery{}).Count(&deliveries).Error)
	require.Equal(t, int64(1), deliveries)

	second, err := RepairLegacyDiscountRates()
	require.NoError(t, err)
	require.Zero(t, second.CorrectedTotal())
}
