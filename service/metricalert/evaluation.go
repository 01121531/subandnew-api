package metricalert

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
)

var ErrDataUnavailable = errors.New("metric alert data unavailable")

type EvaluationResult struct {
	RuleID    int64              `json:"rule_id"`
	Scopes    int                `json:"scopes"`
	Triggered int                `json:"triggered"`
	Recovered int                `json:"recovered"`
	Values    map[string]float64 `json:"values,omitempty"`
}

type scopeEvaluation struct {
	Key          string
	InstanceID   int64
	InstanceName string
	InstanceKind string
	Values       map[string]float64
	ObservedAt   int64
	ErrorCode    string
}

func EvaluateRule(ctx context.Context, ruleID int64) (*EvaluationResult, error) {
	view, err := GetRule(ruleID)
	if err != nil {
		return nil, err
	}
	if !view.Enabled {
		return &EvaluationResult{RuleID: ruleID}, nil
	}
	instances, err := loadInstances(view.InstanceIDs)
	if err != nil {
		return nil, err
	}
	result := &EvaluationResult{RuleID: ruleID, Values: map[string]float64{}}
	scopes := make([]scopeEvaluation, 0, len(instances))
	if view.ScopeMode == ScopeAggregate {
		scopes = append(scopes, evaluateAggregateScope(ctx, instances, view.Conditions))
	} else {
		for _, instance := range instances {
			scopes = append(scopes, evaluateInstanceScope(ctx, instance, view.Conditions))
		}
	}
	result.Scopes = len(scopes)
	for _, scope := range scopes {
		triggered, recovered, evalErr := evaluateScope(view, scope)
		if evalErr != nil && !errors.Is(evalErr, ErrDataUnavailable) {
			return nil, evalErr
		}
		if triggered {
			result.Triggered++
		}
		if recovered {
			result.Recovered++
		}
		for key, value := range scope.Values {
			result.Values[scope.Key+":"+key] = value
		}
	}
	return result, nil
}

func evaluateInstanceScope(_ context.Context, instance model.ManagedInstance, conditions []*model.MetricAlertCondition) scopeEvaluation {
	scope := scopeEvaluation{Key: strconv.FormatInt(instance.Id, 10), InstanceID: instance.Id, InstanceName: instance.Name, InstanceKind: instance.Kind, Values: map[string]float64{}}
	state, ok, err := managedinstance.CurrentManagedRealtime(instance.Id)
	if err != nil {
		scope.ErrorCode = "metric_state_unavailable"
		return scope
	}
	for _, condition := range conditions {
		value, observedAt, available := metricValue(instance, state, ok, condition.Metric)
		if !available {
			scope.ErrorCode = "metric_data_unavailable"
			return scope
		}
		scope.Values[condition.Metric] = value
		if scope.ObservedAt == 0 || observedAt < scope.ObservedAt {
			scope.ObservedAt = observedAt
		}
	}
	return scope
}

func evaluateAggregateScope(ctx context.Context, instances []model.ManagedInstance, conditions []*model.MetricAlertCondition) scopeEvaluation {
	names := make([]string, 0, len(instances))
	for _, instance := range instances {
		names = append(names, instance.Name)
	}
	scope := scopeEvaluation{Key: aggregateScopeKey(instances), InstanceName: strings.Join(names, ", "), InstanceKind: "mixed", Values: map[string]float64{}}
	states := make([]managedinstance.ManagedRealtimeState, 0, len(instances))
	oks := make([]bool, 0, len(instances))
	for _, instance := range instances {
		state, ok, err := managedinstance.CurrentManagedRealtime(instance.Id)
		if err != nil {
			scope.ErrorCode = "metric_state_unavailable"
			return scope
		}
		states = append(states, state)
		oks = append(oks, ok)
	}
	_ = ctx
	for _, condition := range conditions {
		value, observedAt, available := aggregateMetric(instances, states, oks, condition.Metric)
		if !available {
			scope.ErrorCode = "metric_data_unavailable"
			return scope
		}
		scope.Values[condition.Metric] = value
		if scope.ObservedAt == 0 || observedAt < scope.ObservedAt {
			scope.ObservedAt = observedAt
		}
	}
	return scope
}

func metricValue(instance model.ManagedInstance, state managedinstance.ManagedRealtimeState, stateOK bool, metric string) (float64, int64, bool) {
	if metric == "instance_connected" {
		connected := instance.Status == model.ManagedInstanceStatusHealthy || instance.Status == model.ManagedInstanceStatusDegraded
		if connected {
			return 1, max(instance.LastCheckedAt, state.ObservedAt), true
		}
		return 0, max(instance.LastCheckedAt, state.LastAttemptAt), true
	}
	if metric == "unhealthy_instances" {
		value, observedAt, _ := metricValue(instance, state, stateOK, "instance_connected")
		return 1 - value, observedAt, true
	}
	if metric == "today_cost" {
		if value, ok := sampleValue(state.TodayCost); ok && !state.Stale {
			return value, state.ObservedAt, true
		}
		return dashboardTodayCost(instance.Id)
	}
	if metric == "requests" || metric == "tokens" {
		return dashboardMetricValue(instance.Id, metric)
	}
	if !stateOK || state.Stale || state.ObservedAt <= 0 {
		return 0, state.ObservedAt, false
	}
	switch metric {
	case "rpm":
		value, ok := sampleValue(state.RPM)
		return value, state.ObservedAt, ok
	case "rpm_capacity":
		value, ok := sampleValue(state.RPMCapacity)
		return value, state.ObservedAt, ok
	case "rpm_utilization":
		rpm, rpmOK := sampleValue(state.RPM)
		capacity, capacityOK := sampleValue(state.RPMCapacity)
		if !rpmOK || !capacityOK || capacity <= 0 {
			return 0, state.ObservedAt, false
		}
		return rpm / capacity * 100, state.ObservedAt, true
	case "accounts_available":
		if state.AccountsCollectionStatus != model.ManagedInstanceCollectionSucceeded {
			return 0, state.ObservedAt, false
		}
		return float64(state.AccountsAvailable), state.ObservedAt, true
	case "accounts_total":
		if state.AccountsCollectionStatus != model.ManagedInstanceCollectionSucceeded {
			return 0, state.ObservedAt, false
		}
		return float64(state.AccountsTotal), state.ObservedAt, true
	case "accounts_availability":
		if state.AccountsCollectionStatus != model.ManagedInstanceCollectionSucceeded || state.AccountsTotal <= 0 {
			return 0, state.ObservedAt, false
		}
		return float64(state.AccountsAvailable) / float64(state.AccountsTotal) * 100, state.ObservedAt, true
	case "concurrency_used":
		value, ok := sampleValue(state.ConcurrencyUsed)
		return value, state.ObservedAt, ok
	case "concurrency_max":
		value, ok := sampleValue(state.ConcurrencyMax)
		return value, state.ObservedAt, ok
	case "concurrency_utilization":
		used, usedOK := sampleValue(state.ConcurrencyUsed)
		limit, limitOK := sampleValue(state.ConcurrencyMax)
		if !usedOK || !limitOK || limit <= 0 {
			return 0, state.ObservedAt, false
		}
		return used / limit * 100, state.ObservedAt, true
	case "success_rate":
		value, ok := sampleValue(state.SuccessRate)
		return value * 100, state.ObservedAt, ok
	case "active_sessions":
		return float64(state.ActiveSessions), state.ObservedAt, true
	default:
		return 0, state.ObservedAt, false
	}
}

func aggregateMetric(instances []model.ManagedInstance, states []managedinstance.ManagedRealtimeState, oks []bool, metric string) (float64, int64, bool) {
	if metric == "rpm_utilization" {
		rpm, observedAt, rpmOK := aggregateMetric(instances, states, oks, "rpm")
		capacity, capacityAt, capacityOK := aggregateMetric(instances, states, oks, "rpm_capacity")
		return ratio(rpm, capacity), minPositive(observedAt, capacityAt), rpmOK && capacityOK && capacity > 0
	}
	if metric == "accounts_availability" {
		available, observedAt, availableOK := aggregateMetric(instances, states, oks, "accounts_available")
		total, totalAt, totalOK := aggregateMetric(instances, states, oks, "accounts_total")
		return ratio(available, total), minPositive(observedAt, totalAt), availableOK && totalOK && total > 0
	}
	if metric == "concurrency_utilization" {
		used, observedAt, usedOK := aggregateMetric(instances, states, oks, "concurrency_used")
		limit, limitAt, limitOK := aggregateMetric(instances, states, oks, "concurrency_max")
		return ratio(used, limit), minPositive(observedAt, limitAt), usedOK && limitOK && limit > 0
	}
	if metric == "success_rate" {
		total, weight, observedAt := 0.0, 0.0, int64(0)
		for index, instance := range instances {
			value, at, ok := metricValue(instance, states[index], oks[index], metric)
			if !ok {
				return 0, observedAt, false
			}
			itemWeight := states[index].SuccessRateSampleCount
			if itemWeight <= 0 {
				itemWeight = 1
			}
			total += value * itemWeight
			weight += itemWeight
			observedAt = minPositive(observedAt, at)
		}
		return total / weight, observedAt, weight > 0
	}
	total, observedAt := 0.0, int64(0)
	for index, instance := range instances {
		value, at, ok := metricValue(instance, states[index], oks[index], metric)
		if !ok {
			return 0, observedAt, false
		}
		total += value
		observedAt = minPositive(observedAt, at)
	}
	return total, observedAt, true
}

func dashboardTodayCost(instanceID int64) (float64, int64, bool) {
	return dashboardMetricValue(instanceID, "today_cost")
}

func dashboardMetricValue(instanceID int64, metric string) (float64, int64, bool) {
	var snapshot model.ManagedDashboardSnapshot
	if err := model.DB.Where("instance_id = ? AND range_key = ?", instanceID, "preset-1").First(&snapshot).Error; err != nil || snapshot.ObservedAt <= 0 || snapshot.LastAttemptStatus == model.ManagedInstanceCollectionFailed || common.GetTimestamp()-snapshot.ObservedAt > 360 {
		return 0, snapshot.ObservedAt, false
	}
	var summary managedinstance.SummaryResult
	if json.Unmarshal([]byte(snapshot.Payload), &summary) != nil {
		return 0, snapshot.ObservedAt, false
	}
	sample := summary.Cost
	switch metric {
	case "requests":
		sample = summary.Requests
	case "tokens":
		sample = summary.Tokens
	}
	value, ok := sampleValue(sample)
	return value, snapshot.ObservedAt, ok
}

func evaluateScope(view *RuleView, scope scopeEvaluation) (bool, bool, error) {
	state, err := loadOrCreateState(view.ID, scope)
	if err != nil {
		return false, false, err
	}
	if scope.ErrorCode != "" || scope.ObservedAt <= 0 {
		return false, false, recordDataFailure(view, state, scope)
	}
	if state.FailureNotified {
		if _, err := createEvent(view, state, scope, model.MetricAlertEventMonitorRecovery, ""); err != nil {
			return false, false, err
		}
		state.FailureNotified = false
		state.FailureEventID = 0
	}
	state.ConsecutiveFailures = 0
	state.LastErrorCode = ""
	state.LastObservedAt = scope.ObservedAt
	encodedValues, _ := json.Marshal(scope.Values)
	state.LastValues = string(encodedValues)

	violations := make([]bool, 0, len(view.Conditions))
	stillViolating := make([]bool, 0, len(view.Conditions))
	for _, condition := range view.Conditions {
		value := scope.Values[condition.Metric]
		threshold, _ := strconv.ParseFloat(condition.Threshold, 64)
		violations = append(violations, compare(value, threshold, condition.Operator))
		recoveryThreshold := threshold
		if condition.RecoveryThreshold != "" {
			recoveryThreshold, _ = strconv.ParseFloat(condition.RecoveryThreshold, 64)
		}
		stillViolating = append(stillViolating, compare(value, recoveryThreshold, condition.Operator))
	}
	violated := combine(violations, view.MatchMode)
	remainsActive := combine(stillViolating, view.MatchMode)
	triggered, recovered := false, false
	now := common.GetTimestamp()
	if !state.Active {
		if violated {
			state.ConsecutiveViolations++
		} else {
			state.ConsecutiveViolations = 0
		}
		state.ConsecutiveRecoveries = 0
		if state.ConsecutiveViolations >= view.TriggerCount {
			event, eventErr := createEvent(view, state, scope, model.MetricAlertEventTriggered, "")
			if eventErr != nil {
				return false, false, eventErr
			}
			state.Active = true
			state.OpenEventID = event.ID
			state.LastNotifiedAt = now
			state.ConsecutiveViolations = 0
			triggered = true
		}
	} else {
		if remainsActive {
			state.ConsecutiveRecoveries = 0
			if view.ReminderMode == ReminderInterval && view.RepeatIntervalSeconds >= 60 && now-state.LastNotifiedAt >= view.RepeatIntervalSeconds {
				if _, eventErr := createEvent(view, state, scope, model.MetricAlertEventTriggered, "repeat"); eventErr != nil {
					return false, false, eventErr
				}
				state.LastNotifiedAt = now
				triggered = true
			}
		} else {
			state.ConsecutiveRecoveries++
			if state.ConsecutiveRecoveries >= view.RecoveryCount {
				if _, eventErr := createEvent(view, state, scope, model.MetricAlertEventRecovered, ""); eventErr != nil {
					return false, false, eventErr
				}
				state.Active = false
				state.OpenEventID = 0
				state.ConsecutiveRecoveries = 0
				state.LastNotifiedAt = now
				recovered = true
			}
		}
	}
	return triggered, recovered, model.DB.Save(state).Error
}

func recordDataFailure(view *RuleView, state *model.MetricAlertState, scope scopeEvaluation) error {
	state.ConsecutiveFailures++
	state.LastErrorCode = scope.ErrorCode
	if scope.ErrorCode == "" {
		state.LastErrorCode = "metric_data_unavailable"
	}
	if state.ConsecutiveFailures >= view.FailureThreshold && !state.FailureNotified {
		event, err := createEvent(view, state, scope, model.MetricAlertEventMonitorFailure, state.LastErrorCode)
		if err != nil {
			return err
		}
		state.FailureNotified = true
		state.FailureEventID = event.ID
	}
	if err := model.DB.Save(state).Error; err != nil {
		return err
	}
	return ErrDataUnavailable
}

func loadOrCreateState(ruleID int64, scope scopeEvaluation) (*model.MetricAlertState, error) {
	var state model.MetricAlertState
	err := model.DB.Where("rule_id = ? AND scope_key = ?", ruleID, scope.Key).First(&state).Error
	if err == nil {
		return &state, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	state = model.MetricAlertState{RuleID: ruleID, ScopeKey: scope.Key, InstanceID: scope.InstanceID, LastValues: "{}"}
	if err := model.DB.Create(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func createEvent(view *RuleView, state *model.MetricAlertState, scope scopeEvaluation, eventType string, suffix string) (*model.BillingAlertEvent, error) {
	now := common.GetTimestamp()
	conditions, _ := json.Marshal(view.Conditions)
	values, _ := json.Marshal(scope.Values)
	metricKey := ""
	thresholdName := "组合条件"
	threshold := ""
	if len(view.Conditions) == 1 {
		metricKey = view.Conditions[0].Metric
		thresholdName = metricLabel(metricKey)
		threshold = view.Conditions[0].Operator + " " + view.Conditions[0].Threshold
	}
	keyRaw := fmt.Sprintf("metric:%d:%s:%s:%d:%s", view.ID, scope.Key, eventType, now, suffix)
	digest := sha256.Sum256([]byte(keyRaw))
	event := &model.BillingAlertEvent{
		EventKey: "metric-" + hex.EncodeToString(digest[:16]), EventType: eventType, SourceType: model.AlertSourceMetric,
		RuleID: view.ID, InstanceID: scope.InstanceID, RuleName: view.Name, InstanceName: scope.InstanceName,
		InstanceKind: scope.InstanceKind, ThresholdName: thresholdName, Threshold: threshold,
		Recipients: view.MetricAlertRule.Recipients, ErrorCode: scope.ErrorCode, ScopeMode: view.ScopeMode,
		MetricKey: metricKey, Conditions: string(conditions), ObservedValues: string(values), CreatedAt: now,
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(event).Error; err != nil {
			return err
		}
		var recipients []string
		_ = json.Unmarshal([]byte(view.MetricAlertRule.Recipients), &recipients)
		for _, recipient := range recipients {
			delivery := &model.BillingEmailDelivery{EventID: event.ID, Recipient: recipient, Status: "pending", NextRetryAt: now}
			if err := tx.Create(delivery).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return event, err
}

func compare(value float64, threshold float64, operator string) bool {
	switch operator {
	case "gt":
		return value > threshold
	case "gte":
		return value >= threshold
	case "lt":
		return value < threshold
	case "lte":
		return value <= threshold
	case "eq":
		return math.Abs(value-threshold) < 1e-9
	case "ne":
		return math.Abs(value-threshold) >= 1e-9
	default:
		return false
	}
}

func combine(values []bool, mode string) bool {
	if mode == MatchAny {
		for _, value := range values {
			if value {
				return true
			}
		}
		return false
	}
	for _, value := range values {
		if !value {
			return false
		}
	}
	return len(values) > 0
}

func sampleValue(sample managedinstance.MetricSample) (float64, bool) {
	return pointerValue(sample.Value, sample.CollectionStatus == model.ManagedInstanceCollectionSucceeded)
}

func pointerValue(value *float64, ok bool) (float64, bool) {
	if !ok || value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return 0, false
	}
	return *value, true
}

func ratio(value, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return value / total * 100
}

func minPositive(left, right int64) int64 {
	if left <= 0 {
		return right
	}
	if right <= 0 || left < right {
		return left
	}
	return right
}

func aggregateScopeKey(instances []model.ManagedInstance) string {
	parts := make([]string, 0, len(instances))
	for _, instance := range instances {
		parts = append(parts, strconv.FormatInt(instance.Id, 10))
	}
	return "aggregate:" + strings.Join(parts, ",")
}

func metricLabel(key string) string {
	for _, definition := range metricDefinitions {
		if definition.Key == key {
			return definition.Label
		}
	}
	return key
}
