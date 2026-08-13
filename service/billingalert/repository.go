package billingalert

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

const (
	ExchangeModeRecordDate = "record_date"
	ExchangeModeLatest     = "latest"
	ExchangeModeCycleFixed = "cycle_fixed"
	ExchangeModeManual     = "manual"

	ReminderOnce      = "once_per_cycle"
	ReminderInterval  = "repeat_interval"
	ReminderIncrement = "repeat_increment"

	ScheduleInterval   = "interval"
	ScheduleFixedTimes = "fixed_times"
)

var (
	ErrInvalidBillingInput = errors.New("invalid billing alert input")
	ErrBillingNotFound     = errors.New("billing alert resource not found")
)

type TemplateInput struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	SystemKind  string              `json:"system_kind"`
	Filters     map[string][]string `json:"filters"`
}

type TemplateView struct {
	model.BillingFilterTemplate
	Filters map[string][]string `json:"filters"`
}

type ThresholdInput struct {
	Name                  string `json:"name"`
	Severity              string `json:"severity"`
	Currency              string `json:"currency"`
	Amount                string `json:"amount"`
	ReminderMode          string `json:"reminder_mode"`
	RepeatIntervalSeconds int64  `json:"repeat_interval_seconds"`
	RepeatIncrement       string `json:"repeat_increment"`
}

type RuleInput struct {
	Name               string           `json:"name"`
	Description        string           `json:"description"`
	TemplateID         int64            `json:"template_id"`
	Enabled            bool             `json:"enabled"`
	Timezone           string           `json:"timezone"`
	CycleType          string           `json:"cycle_type"`
	CycleConfig        json.RawMessage  `json:"cycle_config"`
	DiscountRate       string           `json:"discount_rate"`
	ExchangeMode       string           `json:"exchange_mode"`
	ManualExchangeRate string           `json:"manual_exchange_rate"`
	ExchangeOverride   bool             `json:"exchange_override"`
	ScheduleType       string           `json:"schedule_type"`
	ScheduleConfig     json.RawMessage  `json:"schedule_config"`
	Recipients         []string         `json:"recipients"`
	FailureThreshold   int              `json:"failure_threshold"`
	InstanceIDs        []int64          `json:"instance_ids"`
	Thresholds         []ThresholdInput `json:"thresholds"`
}

type RuleView struct {
	model.BillingAlertRule
	Recipients  []string                          `json:"recipients"`
	InstanceIDs []int64                           `json:"instance_ids"`
	Thresholds  []*model.BillingAlertThreshold    `json:"thresholds"`
	Template    *TemplateView                     `json:"template,omitempty"`
	Bindings    []*model.BillingAlertRuleInstance `json:"bindings"`
}

type TemplateImpact struct {
	TemplateID      int64    `json:"template_id"`
	CurrentVersion  int      `json:"current_version"`
	NextVersion     int      `json:"next_version"`
	RuleCount       int64    `json:"rule_count"`
	InstanceCount   int64    `json:"instance_count"`
	ResetCycleCount int64    `json:"reset_cycle_count"`
	AddedFields     []string `json:"added_fields"`
	RemovedFields   []string `json:"removed_fields"`
	ChangedFields   []string `json:"changed_fields"`
}

type RuleImpact struct {
	RuleID          int64 `json:"rule_id"`
	TemplateID      int64 `json:"template_id"`
	TemplateVersion int   `json:"template_version"`
	InstanceCount   int   `json:"instance_count"`
	ThresholdCount  int   `json:"threshold_count"`
	ResetCycleCount int64 `json:"reset_cycle_count"`
}

func PreviewRule(id int64, input RuleInput, actorID int) (*RuleImpact, error) {
	prepared, thresholds, instanceIDs, err := prepareRule(input, actorID)
	if err != nil {
		return nil, err
	}
	if err := requireTemplateAndInstances(model.DB, prepared.TemplateID, instanceIDs); err != nil {
		return nil, err
	}
	var template model.BillingFilterTemplate
	if err := model.DB.First(&template, prepared.TemplateID).Error; err != nil {
		return nil, err
	}
	impact := &RuleImpact{
		RuleID: id, TemplateID: prepared.TemplateID, TemplateVersion: template.CurrentVersion,
		InstanceCount: len(instanceIDs), ThresholdCount: len(thresholds),
	}
	if id > 0 {
		if err := model.DB.Model(&model.BillingCycleSnapshot{}).Where("rule_id = ?", id).Count(&impact.ResetCycleCount).Error; err != nil {
			return nil, err
		}
	}
	return impact, nil
}

func CreateTemplate(input TemplateInput, actorID int) (*TemplateView, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || actorID <= 0 {
		return nil, ErrInvalidBillingInput
	}
	systemKind, err := normalizeTemplateSystemKind(input.SystemKind)
	if err != nil {
		return nil, err
	}
	filters, err := normalizeFilters(input.Filters)
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(filters)
	template := &model.BillingFilterTemplate{
		Name: input.Name, Description: strings.TrimSpace(input.Description), SystemKind: systemKind, CurrentVersion: 1,
		Enabled: true, CreatedBy: actorID, UpdatedBy: actorID,
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(template).Error; err != nil {
			return err
		}
		return tx.Create(&model.BillingFilterTemplateVersion{
			TemplateID: template.ID, Version: 1, Filters: string(encoded), CreatedBy: actorID,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &TemplateView{BillingFilterTemplate: *template, Filters: filters}, nil
}

func ListTemplates() ([]*TemplateView, error) {
	var templates []*model.BillingFilterTemplate
	if err := model.DB.Order("id DESC").Find(&templates).Error; err != nil {
		return nil, err
	}
	views := make([]*TemplateView, 0, len(templates))
	for _, template := range templates {
		view, err := templateView(template)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func GetTemplate(id int64) (*TemplateView, error) {
	var template model.BillingFilterTemplate
	if err := model.DB.First(&template, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBillingNotFound
		}
		return nil, err
	}
	return templateView(&template)
}

func PreviewTemplateUpdate(id int64, input TemplateInput) (*TemplateImpact, error) {
	current, err := GetTemplate(id)
	if err != nil {
		return nil, err
	}
	next, err := normalizeFilters(input.Filters)
	if err != nil {
		return nil, err
	}
	impact := &TemplateImpact{TemplateID: id, CurrentVersion: current.CurrentVersion, NextVersion: current.CurrentVersion + 1}
	impact.AddedFields, impact.RemovedFields, impact.ChangedFields = filterDiff(current.Filters, next)
	systemKind, err := normalizeTemplateSystemKind(input.SystemKind)
	if err != nil {
		return nil, err
	}
	if systemKind != "" && systemKind != current.SystemKind {
		if err := validateTemplateBindingsKind(model.DB, id, systemKind); err != nil {
			return nil, err
		}
		impact.ChangedFields = append(impact.ChangedFields, "system_kind")
		sort.Strings(impact.ChangedFields)
	}
	if err := model.DB.Model(&model.BillingAlertRule{}).Where("template_id = ?", id).Count(&impact.RuleCount).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.BillingAlertRuleInstance{}).
		Joins("JOIN billing_alert_rules ON billing_alert_rules.id = billing_alert_rule_instances.rule_id").
		Where("billing_alert_rules.template_id = ?", id).Count(&impact.InstanceCount).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.BillingCycleSnapshot{}).
		Joins("JOIN billing_alert_rules ON billing_alert_rules.id = billing_cycle_snapshots.rule_id").
		Where("billing_alert_rules.template_id = ?", id).Count(&impact.ResetCycleCount).Error; err != nil {
		return nil, err
	}
	return impact, nil
}

func UpdateTemplate(id int64, input TemplateInput, actorID int) (*TemplateView, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || actorID <= 0 {
		return nil, ErrInvalidBillingInput
	}
	systemKind, err := normalizeTemplateSystemKind(input.SystemKind)
	if err != nil {
		return nil, err
	}
	filters, err := normalizeFilters(input.Filters)
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(filters)
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var template model.BillingFilterTemplate
		if err := tx.First(&template, id).Error; err != nil {
			return err
		}
		nextVersion := template.CurrentVersion + 1
		if systemKind == "" {
			systemKind = template.SystemKind
		}
		if err := validateTemplateBindingsKind(tx, id, systemKind); err != nil {
			return err
		}
		if err := tx.Create(&model.BillingFilterTemplateVersion{
			TemplateID: id, Version: nextVersion, Filters: string(encoded), CreatedBy: actorID,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&template).Updates(map[string]any{
			"name": input.Name, "description": strings.TrimSpace(input.Description),
			"system_kind":     systemKind,
			"current_version": nextVersion, "updated_by": actorID, "updated_at": common.GetTimestamp(),
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.BillingCycleSnapshot{}).
			Where("rule_id IN (?)", tx.Model(&model.BillingAlertRule{}).Select("id").Where("template_id = ?", id)).
			Updates(map[string]any{"threshold_state": "{}", "updated_at": common.GetTimestamp()}).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBillingNotFound
		}
		return nil, err
	}
	return GetTemplate(id)
}

func DeleteTemplate(id int64) error {
	var count int64
	if err := model.DB.Model(&model.BillingAlertRule{}).Where("template_id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: template in use", ErrInvalidBillingInput)
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("template_id = ?", id).Delete(&model.BillingFilterTemplateVersion{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.BillingFilterTemplate{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrBillingNotFound
		}
		return nil
	})
}

func CreateRule(input RuleInput, actorID int) (*RuleView, error) {
	prepared, thresholds, instanceIDs, err := prepareRule(input, actorID)
	if err != nil {
		return nil, err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := requireTemplateAndInstances(tx, prepared.TemplateID, instanceIDs); err != nil {
			return err
		}
		if err := tx.Create(prepared).Error; err != nil {
			return err
		}
		return replaceRuleChildren(tx, prepared.ID, thresholds, instanceIDs)
	})
	if err != nil {
		return nil, err
	}
	return GetRule(prepared.ID)
}

func UpdateRule(id int64, input RuleInput, actorID int) (*RuleView, error) {
	prepared, thresholds, instanceIDs, err := prepareRule(input, actorID)
	if err != nil {
		return nil, err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := requireTemplateAndInstances(tx, prepared.TemplateID, instanceIDs); err != nil {
			return err
		}
		updates := map[string]any{
			"name": prepared.Name, "description": prepared.Description, "template_id": prepared.TemplateID,
			"enabled": prepared.Enabled, "timezone": prepared.Timezone, "cycle_type": prepared.CycleType,
			"cycle_config": prepared.CycleConfig, "discount_rate": prepared.DiscountRate,
			"exchange_mode": prepared.ExchangeMode, "manual_exchange_rate": prepared.ManualExchangeRate,
			"exchange_override": prepared.ExchangeOverride, "schedule_type": prepared.ScheduleType,
			"schedule_config": prepared.ScheduleConfig, "recipients": prepared.Recipients,
			"failure_threshold": prepared.FailureThreshold, "updated_by": actorID, "updated_at": common.GetTimestamp(),
		}
		result := tx.Model(&model.BillingAlertRule{}).Where("id = ?", id).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrBillingNotFound
		}
		if err := tx.Where("rule_id = ?", id).Delete(&model.BillingAlertThreshold{}).Error; err != nil {
			return err
		}
		if err := tx.Where("rule_id = ?", id).Delete(&model.BillingAlertRuleInstance{}).Error; err != nil {
			return err
		}
		if err := replaceRuleChildren(tx, id, thresholds, instanceIDs); err != nil {
			return err
		}
		return tx.Model(&model.BillingCycleSnapshot{}).Where("rule_id = ?", id).
			Updates(map[string]any{"threshold_state": "{}", "updated_at": common.GetTimestamp()}).Error
	})
	if err != nil {
		return nil, err
	}
	return GetRule(id)
}

func ListRules() ([]*RuleView, error) {
	var rules []*model.BillingAlertRule
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
	var rule model.BillingAlertRule
	if err := model.DB.First(&rule, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBillingNotFound
		}
		return nil, err
	}
	return ruleView(&rule)
}

func DeleteRule(id int64) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		for _, value := range []any{
			&model.BillingEvaluationSnapshot{},
			&model.BillingCycleSnapshot{}, &model.BillingAlertThreshold{}, &model.BillingAlertRuleInstance{},
		} {
			if err := tx.Where("rule_id = ?", id).Delete(value).Error; err != nil {
				return err
			}
		}
		result := tx.Delete(&model.BillingAlertRule{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrBillingNotFound
		}
		return nil
	})
}

func templateView(template *model.BillingFilterTemplate) (*TemplateView, error) {
	var version model.BillingFilterTemplateVersion
	if err := model.DB.Where("template_id = ? AND version = ?", template.ID, template.CurrentVersion).First(&version).Error; err != nil {
		return nil, err
	}
	filters := map[string][]string{}
	if err := json.Unmarshal([]byte(version.Filters), &filters); err != nil {
		return nil, err
	}
	return &TemplateView{BillingFilterTemplate: *template, Filters: filters}, nil
}

func ruleView(rule *model.BillingAlertRule) (*RuleView, error) {
	view := &RuleView{BillingAlertRule: *rule}
	_ = json.Unmarshal([]byte(rule.Recipients), &view.Recipients)
	if err := model.DB.Where("rule_id = ?", rule.ID).Order("sort_order ASC, id ASC").Find(&view.Thresholds).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Where("rule_id = ?", rule.ID).Order("instance_id ASC").Find(&view.Bindings).Error; err != nil {
		return nil, err
	}
	for _, binding := range view.Bindings {
		view.InstanceIDs = append(view.InstanceIDs, binding.InstanceID)
	}
	template, err := GetTemplate(rule.TemplateID)
	if err != nil {
		return nil, err
	}
	view.Template = template
	return view, nil
}

func prepareRule(input RuleInput, actorID int) (*model.BillingAlertRule, []ThresholdInput, []int64, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || input.TemplateID <= 0 || actorID <= 0 || len(input.InstanceIDs) == 0 || len(input.Thresholds) == 0 {
		return nil, nil, nil, ErrInvalidBillingInput
	}
	if input.Timezone == "" {
		input.Timezone = "Asia/Shanghai"
	}
	if _, err := ResolveCycle(commonTimeNow(), input.Timezone, input.CycleType, string(input.CycleConfig)); err != nil {
		return nil, nil, nil, err
	}
	if _, err := parseNonNegativeDecimal(input.DiscountRate); err != nil {
		return nil, nil, nil, err
	}
	if !validExchangeMode(input.ExchangeMode) || !validSchedule(input.ScheduleType, input.ScheduleConfig) {
		return nil, nil, nil, ErrInvalidBillingInput
	}
	if input.ExchangeMode == ExchangeModeManual {
		rate, err := parseNonNegativeDecimal(input.ManualExchangeRate)
		if err != nil || rate.Sign() == 0 {
			return nil, nil, nil, ErrInvalidBillingInput
		}
	}
	recipients, err := normalizeRecipients(input.Recipients)
	if err != nil {
		return nil, nil, nil, err
	}
	if input.FailureThreshold <= 0 {
		input.FailureThreshold = 3
	}
	for index := range input.Thresholds {
		if err := validateThreshold(input.Thresholds[index]); err != nil {
			return nil, nil, nil, err
		}
	}
	instanceIDs := uniquePositiveIDs(input.InstanceIDs)
	if len(instanceIDs) == 0 {
		return nil, nil, nil, ErrInvalidBillingInput
	}
	recipientsJSON, _ := json.Marshal(recipients)
	return &model.BillingAlertRule{
		Name: input.Name, Description: strings.TrimSpace(input.Description), TemplateID: input.TemplateID,
		Enabled: input.Enabled, Timezone: input.Timezone, CycleType: input.CycleType,
		CycleConfig: normalizedJSON(input.CycleConfig), DiscountRate: input.DiscountRate,
		ExchangeMode: input.ExchangeMode, ManualExchangeRate: input.ManualExchangeRate,
		ExchangeOverride: input.ExchangeOverride, ScheduleType: input.ScheduleType,
		ScheduleConfig: normalizedJSON(input.ScheduleConfig), Recipients: string(recipientsJSON),
		FailureThreshold: input.FailureThreshold, CreatedBy: actorID, UpdatedBy: actorID,
	}, input.Thresholds, instanceIDs, nil
}

func replaceRuleChildren(tx *gorm.DB, ruleID int64, thresholds []ThresholdInput, instanceIDs []int64) error {
	for index, input := range thresholds {
		threshold := &model.BillingAlertThreshold{
			RuleID: ruleID, Name: strings.TrimSpace(input.Name), SortOrder: index,
			Severity: input.Severity, Currency: input.Currency, Amount: input.Amount,
			ReminderMode: input.ReminderMode, RepeatIntervalSeconds: input.RepeatIntervalSeconds,
			RepeatIncrement: input.RepeatIncrement,
		}
		if err := tx.Create(threshold).Error; err != nil {
			return err
		}
	}
	for _, instanceID := range instanceIDs {
		if err := tx.Create(&model.BillingAlertRuleInstance{
			RuleID: ruleID, InstanceID: instanceID, Enabled: true,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func requireTemplateAndInstances(tx *gorm.DB, templateID int64, instanceIDs []int64) error {
	var template model.BillingFilterTemplate
	if err := tx.First(&template, templateID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidBillingInput
		}
		return err
	}
	var instances []model.ManagedInstance
	if err := tx.Where("id IN ?", instanceIDs).Find(&instances).Error; err != nil {
		return err
	}
	if len(instances) != len(instanceIDs) {
		return ErrInvalidBillingInput
	}
	for _, instance := range instances {
		if !templateSupportsInstanceKind(template.SystemKind, instance.Kind) {
			return ErrInvalidBillingInput
		}
	}
	return nil
}

func normalizeTemplateSystemKind(value string) (string, error) {
	value = strings.TrimSpace(value)
	switch value {
	case "", model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindSub2API, model.ManagedInstanceKindConductor:
		return value, nil
	case model.ManagedInstanceKindHuichuan:
		return model.ManagedInstanceKindNewAPI, nil
	default:
		return "", ErrInvalidBillingInput
	}
}

func templateSupportsInstanceKind(templateKind string, instanceKind string) bool {
	if templateKind == "" {
		return true
	}
	if templateKind == model.ManagedInstanceKindNewAPI {
		return instanceKind == model.ManagedInstanceKindNewAPI || instanceKind == model.ManagedInstanceKindHuichuan
	}
	return templateKind == instanceKind
}

func validateTemplateBindingsKind(tx *gorm.DB, templateID int64, systemKind string) error {
	if systemKind == "" {
		return nil
	}
	var instances []model.ManagedInstance
	err := tx.Distinct("managed_instances.*").
		Joins("JOIN billing_alert_rule_instances ON billing_alert_rule_instances.instance_id = managed_instances.id").
		Joins("JOIN billing_alert_rules ON billing_alert_rules.id = billing_alert_rule_instances.rule_id").
		Where("billing_alert_rules.template_id = ?", templateID).
		Find(&instances).Error
	if err != nil {
		return err
	}
	for _, instance := range instances {
		if !templateSupportsInstanceKind(systemKind, instance.Kind) {
			return ErrInvalidBillingInput
		}
	}
	return nil
}

func normalizeFilters(filters map[string][]string) (map[string][]string, error) {
	if len(filters) > 64 {
		return nil, ErrInvalidBillingInput
	}
	result := map[string][]string{}
	for key, values := range filters {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 64 || len(values) > 20 {
			return nil, ErrInvalidBillingInput
		}
		seen := map[string]bool{}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" || len(value) > 512 || seen[value] {
				continue
			}
			seen[value] = true
			result[key] = append(result[key], value)
		}
		if len(result[key]) == 0 {
			delete(result, key)
		}
	}
	return result, nil
}

func normalizeRecipients(values []string) ([]string, error) {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		address, err := mail.ParseAddress(strings.TrimSpace(value))
		if err != nil || address.Address == "" {
			return nil, ErrInvalidBillingInput
		}
		normalized := strings.ToLower(address.Address)
		if !seen[normalized] {
			seen[normalized] = true
			result = append(result, normalized)
		}
	}
	if len(result) == 0 || len(result) > 50 {
		return nil, ErrInvalidBillingInput
	}
	return result, nil
}

func validateThreshold(input ThresholdInput) error {
	if strings.TrimSpace(input.Name) == "" || input.Currency != model.BillingCurrencyUSD && input.Currency != model.BillingCurrencyCNY {
		return ErrInvalidBillingInput
	}
	if input.Severity != "info" && input.Severity != "warning" && input.Severity != "critical" {
		return ErrInvalidBillingInput
	}
	amount, err := parseNonNegativeDecimal(input.Amount)
	if err != nil || amount.Sign() == 0 {
		return ErrInvalidBillingInput
	}
	switch input.ReminderMode {
	case ReminderOnce:
	case ReminderInterval:
		if input.RepeatIntervalSeconds < 60 {
			return ErrInvalidBillingInput
		}
	case ReminderIncrement:
		increment, err := parseNonNegativeDecimal(input.RepeatIncrement)
		if err != nil || increment.Sign() == 0 {
			return ErrInvalidBillingInput
		}
	default:
		return ErrInvalidBillingInput
	}
	return nil
}

func validExchangeMode(value string) bool {
	return value == ExchangeModeRecordDate || value == ExchangeModeLatest || value == ExchangeModeCycleFixed || value == ExchangeModeManual
}

func validSchedule(scheduleType string, config json.RawMessage) bool {
	if scheduleType != ScheduleInterval && scheduleType != ScheduleFixedTimes {
		return false
	}
	var value struct {
		Seconds int64    `json:"seconds"`
		Times   []string `json:"times"`
	}
	if len(config) == 0 || json.Unmarshal(config, &value) != nil {
		return false
	}
	if scheduleType == ScheduleInterval {
		return value.Seconds >= 60 && value.Seconds <= int64((24*time.Hour).Seconds())
	}
	if len(value.Times) == 0 || len(value.Times) > 24 {
		return false
	}
	seen := map[string]bool{}
	for _, configured := range value.Times {
		if _, err := time.Parse("15:04", configured); err != nil || seen[configured] {
			return false
		}
		seen[configured] = true
	}
	return true
}

func normalizedJSON(value json.RawMessage) string {
	var decoded any
	if json.Unmarshal(value, &decoded) != nil {
		return "{}"
	}
	encoded, _ := json.Marshal(decoded)
	return string(encoded)
}

func uniquePositiveIDs(values []int64) []int64 {
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

func filterDiff(current map[string][]string, next map[string][]string) ([]string, []string, []string) {
	added, removed, changed := []string{}, []string{}, []string{}
	for key, nextValues := range next {
		currentValues, exists := current[key]
		if !exists {
			added = append(added, key)
		} else if strings.Join(currentValues, "\x00") != strings.Join(nextValues, "\x00") {
			changed = append(changed, key)
		}
	}
	for key := range current {
		if _, exists := next[key]; !exists {
			removed = append(removed, key)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)
	return added, removed, changed
}

var commonTimeNow = func() time.Time { return time.Now() }
