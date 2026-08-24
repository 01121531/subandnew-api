package metricalert

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

const (
	ScopePerInstance = "per_instance"
	ScopeAggregate   = "aggregate"
	MatchAll         = "all"
	MatchAny         = "any"
	ReminderOnce     = "once"
	ReminderInterval = "repeat_interval"
)

var (
	ErrInvalidInput = errors.New("invalid metric alert input")
	ErrNotFound     = errors.New("metric alert rule not found")
)

type ConditionInput struct {
	Metric            string `json:"metric"`
	Operator          string `json:"operator"`
	Threshold         string `json:"threshold"`
	RecoveryThreshold string `json:"recovery_threshold"`
}

type RuleInput struct {
	Name                      string           `json:"name"`
	Description               string           `json:"description"`
	Enabled                   bool             `json:"enabled"`
	ScopeMode                 string           `json:"scope_mode"`
	MatchMode                 string           `json:"match_mode"`
	EvaluationIntervalSeconds int64            `json:"evaluation_interval_seconds"`
	TriggerCount              int              `json:"trigger_count"`
	RecoveryCount             int              `json:"recovery_count"`
	FailureThreshold          int              `json:"failure_threshold"`
	ReminderMode              string           `json:"reminder_mode"`
	RepeatIntervalSeconds     int64            `json:"repeat_interval_seconds"`
	Recipients                []string         `json:"recipients"`
	InstanceIDs               []int64          `json:"instance_ids"`
	Conditions                []ConditionInput `json:"conditions"`
}

type RuleView struct {
	model.MetricAlertRule
	Recipients  []string                      `json:"recipients"`
	InstanceIDs []int64                       `json:"instance_ids"`
	Conditions  []*model.MetricAlertCondition `json:"conditions"`
	States      []*model.MetricAlertState     `json:"states"`
}

type MetricDefinition struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Unit         string   `json:"unit"`
	Kinds        []string `json:"kinds"`
	Aggregatable bool     `json:"aggregatable"`
}

var metricDefinitions = []MetricDefinition{
	{Key: "rpm", Label: "RPM", Unit: "RPM", Kinds: realtimeKinds(), Aggregatable: true},
	{Key: "rpm_capacity", Label: "RPM 最大容量", Unit: "RPM", Kinds: []string{model.ManagedInstanceKindConductor}, Aggregatable: true},
	{Key: "rpm_utilization", Label: "RPM 容量使用率", Unit: "%", Kinds: []string{model.ManagedInstanceKindConductor}, Aggregatable: true},
	{Key: "accounts_available", Label: "可用账号", Unit: "个", Kinds: accountKinds(), Aggregatable: true},
	{Key: "accounts_total", Label: "全部账号", Unit: "个", Kinds: accountKinds(), Aggregatable: true},
	{Key: "accounts_availability", Label: "账号可用率", Unit: "%", Kinds: accountKinds(), Aggregatable: true},
	{Key: "concurrency_used", Label: "当前并发", Unit: "", Kinds: []string{model.ManagedInstanceKindSub2API, model.ManagedInstanceKindClaudeGateway}, Aggregatable: true},
	{Key: "concurrency_max", Label: "最大并发", Unit: "", Kinds: []string{model.ManagedInstanceKindSub2API, model.ManagedInstanceKindClaudeGateway}, Aggregatable: true},
	{Key: "concurrency_utilization", Label: "并发使用率", Unit: "%", Kinds: []string{model.ManagedInstanceKindSub2API, model.ManagedInstanceKindClaudeGateway}, Aggregatable: true},
	{Key: "success_rate", Label: "成功率", Unit: "%", Kinds: []string{model.ManagedInstanceKindClaudeGateway}, Aggregatable: true},
	{Key: "active_sessions", Label: "活跃会话", Unit: "个", Kinds: []string{model.ManagedInstanceKindConductor, model.ManagedInstanceKindClaudeGateway}, Aggregatable: true},
	{Key: "today_cost", Label: "今日费用", Unit: "USD", Kinds: realtimeKinds(), Aggregatable: true},
	{Key: "instance_connected", Label: "实例连接状态", Unit: "", Kinds: realtimeKinds(), Aggregatable: false},
	{Key: "unhealthy_instances", Label: "异常实例数", Unit: "个", Kinds: realtimeKinds(), Aggregatable: true},
}

func realtimeKinds() []string {
	return []string{model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan, model.ManagedInstanceKindSub2API, model.ManagedInstanceKindConductor, model.ManagedInstanceKindClaudeGateway}
}

func accountKinds() []string {
	return []string{model.ManagedInstanceKindSub2API, model.ManagedInstanceKindConductor, model.ManagedInstanceKindClaudeGateway}
}

func MetricDefinitions() []MetricDefinition {
	result := make([]MetricDefinition, len(metricDefinitions))
	copy(result, metricDefinitions)
	return result
}

func Capabilities(instanceIDs []int64, scopeMode string) ([]MetricDefinition, error) {
	instances, err := loadInstances(instanceIDs)
	if err != nil {
		return nil, err
	}
	result := make([]MetricDefinition, 0, len(metricDefinitions))
	for _, definition := range metricDefinitions {
		if scopeMode == ScopeAggregate && !definition.Aggregatable {
			continue
		}
		supported := true
		for _, instance := range instances {
			if !contains(definition.Kinds, instance.Kind) {
				supported = false
				break
			}
		}
		if supported {
			result = append(result, definition)
		}
	}
	return result, nil
}

func ListRules() ([]*RuleView, error) {
	var rules []*model.MetricAlertRule
	if err := model.DB.Order("id DESC").Find(&rules).Error; err != nil {
		return nil, err
	}
	views := make([]*RuleView, 0, len(rules))
	for _, rule := range rules {
		view, err := ruleView(rule)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func GetRule(id int64) (*RuleView, error) {
	var rule model.MetricAlertRule
	if err := model.DB.First(&rule, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return ruleView(&rule)
}

func CreateRule(input RuleInput, actorID int) (*RuleView, error) {
	rule, instances, conditions, err := prepareRule(input, actorID)
	if err != nil {
		return nil, err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(rule).Error; err != nil {
			return err
		}
		return replaceRuleChildren(tx, rule.ID, instances, conditions)
	})
	if err != nil {
		return nil, err
	}
	return GetRule(rule.ID)
}

func UpdateRule(id int64, input RuleInput, actorID int) (*RuleView, error) {
	rule, instances, conditions, err := prepareRule(input, actorID)
	if err != nil {
		return nil, err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.MetricAlertRule
		if err := tx.First(&current, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		rule.ID = id
		rule.CreatedAt = current.CreatedAt
		rule.CreatedBy = current.CreatedBy
		rule.NextRunAt = common.GetTimestamp()
		if err := tx.Save(rule).Error; err != nil {
			return err
		}
		if err := tx.Where("rule_id = ?", id).Delete(&model.MetricAlertState{}).Error; err != nil {
			return err
		}
		return replaceRuleChildren(tx, id, instances, conditions)
	})
	if err != nil {
		return nil, err
	}
	return GetRule(id)
}

func DeleteRule(id int64) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		for _, value := range []any{&model.MetricAlertState{}, &model.MetricAlertCondition{}, &model.MetricAlertRuleInstance{}} {
			if err := tx.Where("rule_id = ?", id).Delete(value).Error; err != nil {
				return err
			}
		}
		result := tx.Delete(&model.MetricAlertRule{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func DueRules(now int64, limit int) ([]*model.MetricAlertRule, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	var rules []*model.MetricAlertRule
	err := model.DB.Where("enabled = ? AND next_run_at <= ?", true, now).Order("next_run_at ASC, id ASC").Limit(limit).Find(&rules).Error
	return rules, err
}

func TouchNextRun(ruleID int64, now int64, interval int64) error {
	if interval <= 0 {
		interval = 60
	}
	return model.DB.Model(&model.MetricAlertRule{}).Where("id = ?", ruleID).Updates(map[string]any{
		"last_evaluated_at": now, "next_run_at": now + interval, "updated_at": now,
	}).Error
}

func ruleView(rule *model.MetricAlertRule) (*RuleView, error) {
	view := &RuleView{MetricAlertRule: *rule}
	_ = json.Unmarshal([]byte(rule.Recipients), &view.Recipients)
	var bindings []*model.MetricAlertRuleInstance
	if err := model.DB.Where("rule_id = ?", rule.ID).Order("instance_id ASC").Find(&bindings).Error; err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		view.InstanceIDs = append(view.InstanceIDs, binding.InstanceID)
	}
	if err := model.DB.Where("rule_id = ?", rule.ID).Order("sort_order ASC, id ASC").Find(&view.Conditions).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Where("rule_id = ?", rule.ID).Order("scope_key ASC").Find(&view.States).Error; err != nil {
		return nil, err
	}
	return view, nil
}

func prepareRule(input RuleInput, actorID int) (*model.MetricAlertRule, []int64, []*model.MetricAlertCondition, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 128 || actorID <= 0 {
		return nil, nil, nil, ErrInvalidInput
	}
	if input.ScopeMode != ScopePerInstance && input.ScopeMode != ScopeAggregate {
		return nil, nil, nil, ErrInvalidInput
	}
	if input.MatchMode != MatchAll && input.MatchMode != MatchAny {
		return nil, nil, nil, ErrInvalidInput
	}
	if !containsInt64([]int64{10, 30, 60, 300}, input.EvaluationIntervalSeconds) || input.TriggerCount < 1 || input.TriggerCount > 100 || input.RecoveryCount < 1 || input.RecoveryCount > 100 || input.FailureThreshold < 1 || input.FailureThreshold > 100 {
		return nil, nil, nil, ErrInvalidInput
	}
	if input.ReminderMode != ReminderOnce && input.ReminderMode != ReminderInterval {
		return nil, nil, nil, ErrInvalidInput
	}
	if input.ReminderMode == ReminderInterval && input.RepeatIntervalSeconds < 60 {
		return nil, nil, nil, ErrInvalidInput
	}
	recipients, err := normalizeRecipients(input.Recipients)
	if err != nil {
		return nil, nil, nil, err
	}
	instances := uniqueIDs(input.InstanceIDs)
	if len(instances) == 0 || len(instances) > 100 {
		return nil, nil, nil, ErrInvalidInput
	}
	capabilities, err := Capabilities(instances, input.ScopeMode)
	if err != nil {
		return nil, nil, nil, err
	}
	allowed := map[string]bool{}
	for _, definition := range capabilities {
		allowed[definition.Key] = true
	}
	if len(input.Conditions) == 0 || len(input.Conditions) > 20 {
		return nil, nil, nil, ErrInvalidInput
	}
	conditions := make([]*model.MetricAlertCondition, 0, len(input.Conditions))
	for index, condition := range input.Conditions {
		condition.Metric = strings.TrimSpace(condition.Metric)
		condition.Operator = strings.TrimSpace(condition.Operator)
		if !allowed[condition.Metric] || !contains([]string{"gt", "gte", "lt", "lte", "eq", "ne"}, condition.Operator) {
			return nil, nil, nil, ErrInvalidInput
		}
		threshold, err := normalizeNumber(condition.Threshold)
		if err != nil {
			return nil, nil, nil, err
		}
		recovery := ""
		if strings.TrimSpace(condition.RecoveryThreshold) != "" {
			recovery, err = normalizeNumber(condition.RecoveryThreshold)
			if err != nil {
				return nil, nil, nil, err
			}
		}
		conditions = append(conditions, &model.MetricAlertCondition{Metric: condition.Metric, Operator: condition.Operator, Threshold: threshold, RecoveryThreshold: recovery, SortOrder: index})
	}
	recipientsJSON, _ := json.Marshal(recipients)
	now := common.GetTimestamp()
	return &model.MetricAlertRule{
		Name: input.Name, Description: strings.TrimSpace(input.Description), Enabled: input.Enabled,
		ScopeMode: input.ScopeMode, MatchMode: input.MatchMode, EvaluationIntervalSeconds: input.EvaluationIntervalSeconds,
		TriggerCount: input.TriggerCount, RecoveryCount: input.RecoveryCount, FailureThreshold: input.FailureThreshold,
		ReminderMode: input.ReminderMode, RepeatIntervalSeconds: input.RepeatIntervalSeconds,
		Recipients: string(recipientsJSON), NextRunAt: now, CreatedBy: actorID, UpdatedBy: actorID,
	}, instances, conditions, nil
}

func replaceRuleChildren(tx *gorm.DB, ruleID int64, instanceIDs []int64, conditions []*model.MetricAlertCondition) error {
	if err := tx.Where("rule_id = ?", ruleID).Delete(&model.MetricAlertRuleInstance{}).Error; err != nil {
		return err
	}
	if err := tx.Where("rule_id = ?", ruleID).Delete(&model.MetricAlertCondition{}).Error; err != nil {
		return err
	}
	for _, instanceID := range instanceIDs {
		if err := tx.Create(&model.MetricAlertRuleInstance{RuleID: ruleID, InstanceID: instanceID}).Error; err != nil {
			return err
		}
	}
	for _, condition := range conditions {
		condition.ID = 0
		condition.RuleID = ruleID
		if err := tx.Create(condition).Error; err != nil {
			return err
		}
	}
	return nil
}

func loadInstances(ids []int64) ([]model.ManagedInstance, error) {
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return nil, ErrInvalidInput
	}
	var instances []model.ManagedInstance
	if err := model.DB.Where("id IN ?", ids).Order("id ASC").Find(&instances).Error; err != nil {
		return nil, err
	}
	if len(instances) != len(ids) {
		return nil, ErrInvalidInput
	}
	return instances, nil
}

func normalizeRecipients(values []string) ([]string, error) {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		address, err := mail.ParseAddress(strings.TrimSpace(value))
		if err != nil || address.Address == "" {
			return nil, ErrInvalidInput
		}
		key := strings.ToLower(address.Address)
		if !seen[key] {
			seen[key] = true
			result = append(result, key)
		}
	}
	if len(result) == 0 || len(result) > 50 {
		return nil, ErrInvalidInput
	}
	return result, nil
}

func normalizeNumber(value string) (string, error) {
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || number != number || number > 1.7976931348623157e+308 || number < -1.7976931348623157e+308 {
		return "", ErrInvalidInput
	}
	return strconv.FormatFloat(number, 'f', -1, 64), nil
}

func uniqueIDs(values []int64) []int64 {
	seen := map[int64]bool{}
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value > 0 && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func containsInt64(values []int64, value int64) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func Audit(actorID int, action string, resourceID int64, outcome string, details any) {
	encoded, _ := json.Marshal(details)
	_ = model.DB.Create(&model.BillingAudit{ActorID: actorID, Action: action, Resource: "metric_alert_rule", ResourceID: resourceID, Outcome: outcome, Details: string(encoded)}).Error
}

func ErrorMessage(err error) string {
	if errors.Is(err, ErrInvalidInput) {
		return "invalid_metric_alert_input"
	}
	if errors.Is(err, ErrNotFound) {
		return "metric_alert_not_found"
	}
	return fmt.Sprintf("metric_alert_operation_failed: %v", err)
}
