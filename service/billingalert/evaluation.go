package billingalert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EvaluationResult struct {
	RuleID       int64   `json:"rule_id"`
	InstanceID   int64   `json:"instance_id"`
	CycleID      int64   `json:"cycle_id"`
	EvaluationID int64   `json:"evaluation_id"`
	USDTotal     string  `json:"usd_total"`
	CNYTotal     string  `json:"cny_total"`
	ExchangeRate string  `json:"exchange_rate"`
	Triggered    []int64 `json:"triggered_threshold_ids"`
	DataTime     int64   `json:"data_timestamp"`
}

type thresholdNotificationState struct {
	LastNotifiedAt int64  `json:"last_notified_at"`
	LastAmount     string `json:"last_amount"`
	Sequence       int    `json:"sequence"`
}

var usageSummaryFetcher = managedinstance.GetUsageRecordSummary

func EvaluateRuleInstance(ctx context.Context, ruleID int64, instanceID int64, now time.Time) (*EvaluationResult, error) {
	rule, binding, instance, template, thresholds, err := loadEvaluationContext(ruleID, instanceID)
	if err != nil {
		return nil, err
	}
	window, err := ResolveCycle(now, rule.Timezone, rule.CycleType, rule.CycleConfig)
	if err != nil || !window.Active {
		if err == nil {
			err = ErrInvalidCycle
		}
		return nil, recordEvaluationFailure(rule, binding, instance, err, now)
	}
	cycle, filters, err := ensureCycleSnapshot(rule, instance, template, window)
	if err != nil {
		return nil, recordEvaluationFailure(rule, binding, instance, err, now)
	}
	filterQuery := evaluationQuery(instance.Kind, filters, window.Start, min(window.End, now.Unix()), rule.Timezone)
	unsupportedFilters := managedinstance.UnsupportedUsageRecordFilterFields(instance.Kind, filterQuery)
	var usdTotal, cnyTotal, rate string
	var rateID, recordCount int64
	if len(unsupportedFilters) > 0 {
		usdTotal, cnyTotal, recordCount = "0.00000000", "0.00000000", 0
		rate, rateID, err = resolveEvaluationRate(rule, cycle)
	} else {
		usdTotal, cnyTotal, rate, rateID, recordCount, err = evaluateAmounts(ctx, rule, instance, cycle, filters, window, now)
	}
	if err != nil {
		return nil, recordEvaluationFailure(rule, binding, instance, err, now)
	}

	result := &EvaluationResult{
		RuleID: rule.ID, InstanceID: instance.Id, CycleID: cycle.ID,
		USDTotal: usdTotal, CNYTotal: cnyTotal, ExchangeRate: rate, DataTime: now.Unix(),
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var lockedCycle model.BillingCycleSnapshot
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedCycle, cycle.ID).Error; err != nil {
			return err
		}
		evaluation := &model.BillingEvaluationSnapshot{
			CycleID: cycle.ID, RuleID: rule.ID, InstanceID: instance.Id,
			USDTotal: usdTotal, CNYTotal: cnyTotal, DiscountRate: cycle.DiscountRate,
			ExchangeRate: rate, ExchangeRateID: rateID, RecordCount: recordCount, DataTimestamp: now.Unix(),
		}
		if err := tx.Create(evaluation).Error; err != nil {
			return err
		}
		result.EvaluationID = evaluation.ID
		state := map[string]thresholdNotificationState{}
		_ = json.Unmarshal([]byte(lockedCycle.ThresholdState), &state)
		for _, threshold := range thresholds {
			amount := usdTotal
			if threshold.Currency == model.BillingCurrencyCNY {
				amount = cnyTotal
			}
			current := state[strconv.FormatInt(threshold.ID, 10)]
			if !thresholdShouldNotify(threshold, amount, current, now.Unix()) {
				continue
			}
			current.LastNotifiedAt = now.Unix()
			current.LastAmount = amount
			current.Sequence++
			state[strconv.FormatInt(threshold.ID, 10)] = current
			event := &model.BillingAlertEvent{
				EventKey:  fmt.Sprintf("threshold:%d:%d:%d", cycle.ID, threshold.ID, current.Sequence),
				EventType: model.BillingAlertEventThreshold, RuleID: rule.ID, InstanceID: instance.Id,
				CycleID: cycle.ID, ThresholdID: threshold.ID, EvaluationID: evaluation.ID,
				RuleName: rule.Name, InstanceName: instance.Name, InstanceKind: instance.Kind,
				ThresholdName: threshold.Name, CycleStart: cycle.CycleStart, CycleEnd: cycle.CycleEnd,
				Timezone: cycle.Timezone, Filters: cycle.Filters, ExchangeMode: cycle.ExchangeMode,
				Currency: threshold.Currency, Threshold: threshold.Amount,
				USDTotal: usdTotal, CNYTotal: cnyTotal, DiscountRate: cycle.DiscountRate,
				ExchangeRate: rate, Recipients: rule.Recipients,
			}
			if rateID > 0 {
				var rateRecord model.ExchangeRate
				if tx.First(&rateRecord, rateID).Error == nil {
					event.ExchangeSource = rateRecord.Source
					event.ExchangeObservedDate = rateRecord.ObservedDate
				}
			}
			if err := tx.Create(event).Error; err != nil {
				return err
			}
			if err := createEmailDeliveries(tx, event.ID, rule.Recipients, now.Unix()); err != nil {
				return err
			}
			result.Triggered = append(result.Triggered, threshold.ID)
		}
		encodedState, _ := json.Marshal(state)
		if err := tx.Model(&lockedCycle).Updates(map[string]any{
			"threshold_state": string(encodedState), "updated_at": now.Unix(),
		}).Error; err != nil {
			return err
		}
		if binding.FailureNotified {
			if err := createMonitorEvent(tx, rule, instance, model.BillingAlertEventRecovery, "", now.Unix()); err != nil {
				return err
			}
		}
		filterStatus := ""
		if len(unsupportedFilters) > 0 {
			filterStatus = "unmatched:" + strings.Join(unsupportedFilters, ",")
		}
		return tx.Model(&model.BillingAlertRuleInstance{}).Where("id = ?", binding.ID).Updates(map[string]any{
			"consecutive_failures": 0, "failure_notified": false, "last_error_code": "",
			"filter_status":     filterStatus,
			"last_evaluated_at": now.Unix(), "last_succeeded_at": now.Unix(),
			"next_run_at": NextEvaluationTime(rule, now).Unix(), "updated_at": now.Unix(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func loadEvaluationContext(ruleID int64, instanceID int64) (*model.BillingAlertRule, *model.BillingAlertRuleInstance, *model.ManagedInstance, *model.BillingFilterTemplateVersion, []*model.BillingAlertThreshold, error) {
	var rule model.BillingAlertRule
	if err := model.DB.First(&rule, ruleID).Error; err != nil || !rule.Enabled {
		return nil, nil, nil, nil, nil, ErrBillingNotFound
	}
	var binding model.BillingAlertRuleInstance
	if err := model.DB.Where("rule_id = ? AND instance_id = ?", ruleID, instanceID).First(&binding).Error; err != nil || !binding.Enabled {
		return nil, nil, nil, nil, nil, ErrBillingNotFound
	}
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, instanceID).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	var template model.BillingFilterTemplate
	if err := model.DB.First(&template, rule.TemplateID).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	var version model.BillingFilterTemplateVersion
	if err := model.DB.Where("template_id = ? AND version = ?", template.ID, template.CurrentVersion).First(&version).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	var thresholds []*model.BillingAlertThreshold
	if err := model.DB.Where("rule_id = ?", rule.ID).Order("sort_order ASC, id ASC").Find(&thresholds).Error; err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return &rule, &binding, &instance, &version, thresholds, nil
}

func ensureCycleSnapshot(rule *model.BillingAlertRule, instance *model.ManagedInstance, template *model.BillingFilterTemplateVersion, window CycleWindow) (*model.BillingCycleSnapshot, map[string][]string, error) {
	filters := map[string][]string{}
	if err := json.Unmarshal([]byte(template.Filters), &filters); err != nil {
		return nil, nil, err
	}
	cycle := &model.BillingCycleSnapshot{
		RuleID: rule.ID, InstanceID: instance.Id, CycleKey: window.Key,
		CycleStart: window.Start, CycleEnd: window.End, Timezone: rule.Timezone,
		TemplateVersion: template.Version, Filters: template.Filters,
		DiscountRate: rule.DiscountRate, ExchangeMode: effectiveExchangeMode(rule), ThresholdState: "{}",
	}
	if cycle.ExchangeMode == ExchangeModeCycleFixed {
		rate, err := LatestStoredExchangeRate()
		if err != nil || rate == nil {
			return nil, nil, ErrExchangeRateUnavailable
		}
		cycle.ExchangeRate = rate.Rate
		cycle.ExchangeRateID = rate.ID
	}
	err := model.DB.Where("rule_id = ? AND instance_id = ? AND cycle_key = ?", rule.ID, instance.Id, window.Key).
		FirstOrCreate(cycle).Error
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal([]byte(cycle.Filters), &filters); err != nil {
		return nil, nil, err
	}
	return cycle, filters, nil
}

func evaluateAmounts(ctx context.Context, rule *model.BillingAlertRule, instance *model.ManagedInstance, cycle *model.BillingCycleSnapshot, filters map[string][]string, window CycleWindow, now time.Time) (string, string, string, int64, int64, error) {
	end := window.End
	if now.Unix() < end {
		end = now.Unix()
	}
	mode := cycle.ExchangeMode
	if mode == ExchangeModeRecordDate {
		return evaluateRecordDateAmounts(ctx, rule, instance, filters, window.Start, end)
	}
	query := evaluationQuery(instance.Kind, filters, window.Start, end, rule.Timezone)
	summary, err := usageSummaryFetcher(ctx, instance.Id, query)
	if err != nil {
		return "", "", "", 0, 0, err
	}
	if !strings.EqualFold(summary.Currency, model.BillingCurrencyUSD) {
		return "", "", "", 0, 0, errors.New("billing_summary_currency_not_usd")
	}
	usd := strconv.FormatFloat(summary.Amount, 'f', 8, 64)
	rate, rateID, err := resolveEvaluationRate(rule, cycle)
	if err != nil {
		return "", "", "", 0, 0, err
	}
	cny, err := CalculateCNY(usd, cycle.DiscountRate, rate)
	if err != nil {
		return "", "", "", 0, 0, err
	}
	return usd, cny, rate, rateID, 0, nil
}

func evaluateRecordDateAmounts(ctx context.Context, rule *model.BillingAlertRule, instance *model.ManagedInstance, filters map[string][]string, start int64, end int64) (string, string, string, int64, int64, error) {
	location, _ := time.LoadLocation(rule.Timezone)
	cursor := time.Unix(start, 0).In(location)
	finish := time.Unix(end, 0).In(location)
	usdTotal := "0"
	cnyTotal := "0"
	var latestRate string
	var latestRateID int64
	var recordCount int64
	for cursor.Before(finish) {
		next := time.Date(cursor.Year(), cursor.Month(), cursor.Day()+1, 0, 0, 0, 0, location)
		if next.After(finish) {
			next = finish
		}
		summary, err := usageSummaryFetcher(ctx, instance.Id, evaluationQuery(instance.Kind, filters, cursor.Unix(), next.Unix(), rule.Timezone))
		if err != nil {
			return "", "", "", 0, 0, err
		}
		usd := strconv.FormatFloat(summary.Amount, 'f', 8, 64)
		rateRecord, err := ExchangeRateOnOrBefore(cursor.Format("2006-01-02"))
		if err != nil || rateRecord == nil {
			return "", "", "", 0, 0, ErrExchangeRateUnavailable
		}
		cny, err := CalculateCNY(usd, rule.DiscountRate, rateRecord.Rate)
		if err != nil {
			return "", "", "", 0, 0, err
		}
		usdTotal, _ = AddDecimal(usdTotal, usd)
		cnyTotal, _ = AddDecimal(cnyTotal, cny)
		latestRate, latestRateID = rateRecord.Rate, rateRecord.ID
		cursor = next
	}
	return usdTotal, cnyTotal, latestRate, latestRateID, recordCount, nil
}

func evaluationQuery(kind string, filters map[string][]string, start int64, end int64, timezone string) url.Values {
	query := url.Values{}
	for key, values := range filters {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	if kind == model.ManagedInstanceKindSub2API {
		location, _ := time.LoadLocation(timezone)
		query.Set("start_date", time.Unix(start, 0).In(location).Format("2006-01-02"))
		query.Set("end_date", time.Unix(end, 0).In(location).Format("2006-01-02"))
		query.Set("timezone", timezone)
	} else {
		query.Set("start_timestamp", strconv.FormatInt(start, 10))
		query.Set("end_timestamp", strconv.FormatInt(end, 10))
	}
	return query
}

func resolveEvaluationRate(rule *model.BillingAlertRule, cycle *model.BillingCycleSnapshot) (string, int64, error) {
	if cycle.ExchangeMode == ExchangeModeManual {
		if rule.ManualExchangeRate != "" {
			return rule.ManualExchangeRate, 0, nil
		}
		setting, err := EnsureExchangeRateSetting()
		if err != nil || setting.ManualRate == "" {
			return "", 0, ErrExchangeRateUnavailable
		}
		return setting.ManualRate, 0, nil
	}
	if cycle.ExchangeMode == ExchangeModeCycleFixed && cycle.ExchangeRate != "" {
		return cycle.ExchangeRate, cycle.ExchangeRateID, nil
	}
	rate, err := LatestStoredExchangeRate()
	if err != nil || rate == nil {
		return "", 0, ErrExchangeRateUnavailable
	}
	return rate.Rate, rate.ID, nil
}

func effectiveExchangeMode(rule *model.BillingAlertRule) string {
	if rule.ExchangeOverride {
		return rule.ExchangeMode
	}
	setting, err := EnsureExchangeRateSetting()
	if err == nil && validExchangeMode(setting.DefaultMode) {
		return setting.DefaultMode
	}
	return rule.ExchangeMode
}

func thresholdShouldNotify(threshold *model.BillingAlertThreshold, amount string, state thresholdNotificationState, now int64) bool {
	comparison, err := CompareDecimal(amount, threshold.Amount)
	if err != nil || comparison < 0 {
		return false
	}
	switch threshold.ReminderMode {
	case ReminderOnce:
		return state.LastNotifiedAt == 0
	case ReminderInterval:
		return state.LastNotifiedAt == 0 || now-state.LastNotifiedAt >= threshold.RepeatIntervalSeconds
	case ReminderIncrement:
		if state.LastNotifiedAt == 0 {
			return true
		}
		difference, err := subtractDecimal(amount, state.LastAmount)
		if err != nil {
			return false
		}
		comparison, err := CompareDecimal(difference, threshold.RepeatIncrement)
		return err == nil && comparison >= 0
	}
	return false
}

func subtractDecimal(left string, right string) (string, error) {
	l, err := parseNonNegativeDecimal(left)
	if err != nil {
		return "", err
	}
	r, err := parseNonNegativeDecimal(right)
	if err != nil {
		return "", err
	}
	return formatDecimal(l.Sub(l, r), MoneyScale), nil
}

func createEmailDeliveries(tx *gorm.DB, eventID int64, encodedRecipients string, now int64) error {
	var recipients []string
	if err := json.Unmarshal([]byte(encodedRecipients), &recipients); err != nil {
		return err
	}
	for _, recipient := range recipients {
		if err := tx.Create(&model.BillingEmailDelivery{
			EventID: eventID, Recipient: recipient, Status: "pending", NextRetryAt: now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func recordEvaluationFailure(rule *model.BillingAlertRule, binding *model.BillingAlertRuleInstance, instance *model.ManagedInstance, cause error, now time.Time) error {
	code := billingErrorCode(cause)
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var locked model.BillingAlertRuleInstance
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, binding.ID).Error; err != nil {
			return err
		}
		failures := locked.ConsecutiveFailures + 1
		notified := locked.FailureNotified
		if failures >= rule.FailureThreshold && !notified {
			if err := createMonitorEvent(tx, rule, instance, model.BillingAlertEventFailure, code, now.Unix()); err != nil {
				return err
			}
			notified = true
		}
		return tx.Model(&locked).Updates(map[string]any{
			"consecutive_failures": failures, "failure_notified": notified,
			"last_error_code": code, "last_evaluated_at": now.Unix(),
			"next_run_at": NextEvaluationTime(rule, now).Unix(), "updated_at": now.Unix(),
		}).Error
	})
	if err != nil {
		return err
	}
	return cause
}

func createMonitorEvent(tx *gorm.DB, rule *model.BillingAlertRule, instance *model.ManagedInstance, eventType string, errorCode string, now int64) error {
	event := &model.BillingAlertEvent{
		EventKey:  fmt.Sprintf("%s:%d:%d:%d", eventType, rule.ID, instance.Id, now),
		EventType: eventType, RuleID: rule.ID, InstanceID: instance.Id,
		RuleName: rule.Name, InstanceName: instance.Name, InstanceKind: instance.Kind,
		Timezone: rule.Timezone, Recipients: rule.Recipients, ErrorCode: errorCode,
	}
	if err := tx.Create(event).Error; err != nil {
		return err
	}
	return createEmailDeliveries(tx, event.ID, rule.Recipients, now)
}

func billingErrorCode(err error) string {
	if err == nil {
		return "unknown"
	}
	switch {
	case errors.Is(err, ErrExchangeRateUnavailable):
		return "exchange_rate_unavailable"
	case errors.Is(err, ErrInvalidDiscountRate):
		return "invalid_discount_rate"
	case errors.Is(err, context.DeadlineExceeded):
		return "target_timeout"
	case errors.Is(err, gorm.ErrRecordNotFound):
		return "data_not_found"
	default:
		return "collection_failed"
	}
}

func NextEvaluationTime(rule *model.BillingAlertRule, now time.Time) time.Time {
	location, err := time.LoadLocation(rule.Timezone)
	if err != nil {
		location = time.UTC
	}
	localNow := now.In(location)
	var config struct {
		Seconds int64    `json:"seconds"`
		Times   []string `json:"times"`
	}
	_ = json.Unmarshal([]byte(rule.ScheduleConfig), &config)
	if rule.ScheduleType == ScheduleInterval {
		if config.Seconds < 60 {
			config.Seconds = 300
		}
		return now.Add(time.Duration(config.Seconds) * time.Second)
	}
	var next time.Time
	for offset := 0; offset <= 1; offset++ {
		day := localNow.AddDate(0, 0, offset)
		for _, value := range config.Times {
			parsed, err := time.Parse("15:04", value)
			if err != nil {
				continue
			}
			candidate := time.Date(day.Year(), day.Month(), day.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
			if candidate.After(localNow) && (next.IsZero() || candidate.Before(next)) {
				next = candidate
			}
		}
	}
	if next.IsZero() {
		next = localNow.Add(24 * time.Hour)
	}
	return next
}

func DueRuleBindings(now int64, limit int) ([]*model.BillingAlertRuleInstance, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var bindings []*model.BillingAlertRuleInstance
	err := model.DB.Joins("JOIN billing_alert_rules ON billing_alert_rules.id = billing_alert_rule_instances.rule_id").
		Where("billing_alert_rule_instances.enabled = ? AND billing_alert_rules.enabled = ? AND billing_alert_rule_instances.next_run_at <= ?", true, true, now).
		Order("billing_alert_rule_instances.next_run_at ASC, billing_alert_rule_instances.id ASC").Limit(limit).Find(&bindings).Error
	return bindings, err
}
