package managedinstance

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAlertRuleNotFound = errors.New("managed instance alert rule not found")
	ErrAlertRuleConflict = errors.New("managed instance alert rule instance conflict")
)

type AlertRuleInput struct {
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	Enabled              bool     `json:"enabled"`
	AlertTypes           []string `json:"alert_types"`
	CheckIntervalSeconds int      `json:"check_interval_seconds"`
	FailureThreshold     int      `json:"failure_threshold"`
	Recipients           []string `json:"recipients"`
	InstanceIDs          []int64  `json:"instance_ids"`
}

type AlertRuleView struct {
	ID                        int64    `json:"id"`
	Name                      string   `json:"name"`
	Description               string   `json:"description"`
	Enabled                   bool     `json:"enabled"`
	AlertTypes                []string `json:"alert_types"`
	CheckIntervalSeconds      int      `json:"check_interval_seconds"`
	FailureThreshold          int      `json:"failure_threshold"`
	EffectiveFailureThreshold int      `json:"effective_failure_threshold"`
	Recipients                []string `json:"recipients"`
	EffectiveRecipients       []string `json:"effective_recipients"`
	InstanceIDs               []int64  `json:"instance_ids"`
	CreatedBy                 int      `json:"created_by"`
	UpdatedBy                 int      `json:"updated_by"`
	CreatedAt                 int64    `json:"created_at"`
	UpdatedAt                 int64    `json:"updated_at"`
}

type AlertRuleConflictError struct{ InstanceIDs []int64 }

func (err *AlertRuleConflictError) Error() string {
	return fmt.Sprintf("%s: %v", ErrAlertRuleConflict, err.InstanceIDs)
}
func (err *AlertRuleConflictError) Unwrap() error { return ErrAlertRuleConflict }

type activeAlertPolicy struct {
	RuleID              int64
	RuleName            string
	AlertTypes          map[string]bool
	FailureThreshold    int
	Recipients          []string
	NotificationEnabled bool
}

func ListAlertRules() ([]*AlertRuleView, error) {
	var rules []*model.ManagedInstanceAlertRule
	if err := model.DB.Order("id DESC").Find(&rules).Error; err != nil {
		return nil, err
	}
	result := make([]*AlertRuleView, 0, len(rules))
	for _, rule := range rules {
		view, err := alertRuleView(model.DB, rule)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}

func GetAlertRule(id int64) (*AlertRuleView, error) {
	var rule model.ManagedInstanceAlertRule
	if err := model.DB.First(&rule, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAlertRuleNotFound
		}
		return nil, err
	}
	return alertRuleView(model.DB, &rule)
}

func CreateAlertRule(input AlertRuleInput, actorID int) (*AlertRuleView, error) {
	normalized, err := normalizeAlertRuleInput(input, actorID)
	if err != nil {
		return nil, err
	}
	rule := &model.ManagedInstanceAlertRule{}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		*rule = alertRuleModel(normalized, actorID)
		if err := tx.Create(rule).Error; err != nil {
			return err
		}
		if err := replaceAlertRuleScope(tx, rule.ID, normalized.InstanceIDs); err != nil {
			return err
		}
		if rule.Enabled {
			return activateAlertRule(tx, rule, normalized.InstanceIDs)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return GetAlertRule(rule.ID)
}

func UpdateAlertRule(id int64, input AlertRuleInput, actorID int) (*AlertRuleView, error) {
	normalized, err := normalizeAlertRuleInput(input, actorID)
	if err != nil {
		return nil, err
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.ManagedInstanceAlertRule
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAlertRuleNotFound
			}
			return err
		}
		oldIDs, err := alertRuleInstanceIDs(tx, id)
		if err != nil {
			return err
		}
		oldTypes := decodeAlertTypes(current.AlertTypes)
		updated := alertRuleModel(normalized, actorID)
		if err := tx.Model(&current).Updates(map[string]any{
			"name": updated.Name, "description": updated.Description, "enabled": updated.Enabled,
			"alert_types": updated.AlertTypes, "check_interval_seconds": updated.CheckIntervalSeconds,
			"failure_threshold": updated.FailureThreshold, "recipients": updated.Recipients,
			"updated_by": actorID, "updated_at": common.GetTimestamp(),
		}).Error; err != nil {
			return err
		}
		if err := replaceAlertRuleScope(tx, id, normalized.InstanceIDs); err != nil {
			return err
		}
		if err := tx.Where("rule_id = ?", id).Delete(&model.ManagedInstanceAlertAssignment{}).Error; err != nil {
			return err
		}
		current = updated
		current.ID = id
		if normalized.Enabled {
			if err := activateAlertRule(tx, &current, normalized.InstanceIDs); err != nil {
				return err
			}
		}
		removedIDs := differenceInt64(oldIDs, normalized.InstanceIDs)
		removedTypes := differenceStrings(oldTypes, normalized.AlertTypes)
		if !normalized.Enabled || len(removedIDs) > 0 || len(removedTypes) > 0 {
			return cancelAlertRuleNotifications(tx, id, removedIDs, removedTypes, !normalized.Enabled)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return GetAlertRule(id)
}

func DeleteAlertRule(id int64) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var rule model.ManagedInstanceAlertRule
		if err := tx.First(&rule, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAlertRuleNotFound
			}
			return err
		}
		if err := cancelAlertRuleNotifications(tx, id, nil, nil, true); err != nil {
			return err
		}
		if err := tx.Where("rule_id = ?", id).Delete(&model.ManagedInstanceAlertAssignment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("rule_id = ?", id).Delete(&model.ManagedInstanceAlertRuleInstance{}).Error; err != nil {
			return err
		}
		return tx.Delete(&rule).Error
	})
}

func normalizeAlertRuleInput(input AlertRuleInput, actorID int) (AlertRuleInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if actorID <= 0 || input.Name == "" || len(input.Name) > 128 || len(input.Description) > 2000 ||
		input.CheckIntervalSeconds < 10 || input.CheckIntervalSeconds > 86400 ||
		input.FailureThreshold < 0 || input.FailureThreshold > 100 {
		return input, ErrInvalidInstance
	}
	input.InstanceIDs = uniquePositiveIDs(input.InstanceIDs)
	if len(input.InstanceIDs) == 0 || len(input.InstanceIDs) > 100 {
		return input, ErrInvalidInstance
	}
	types := make(map[string]bool)
	for _, value := range input.AlertTypes {
		if value != model.ManagedInstanceAlertTypeAvailability && value != model.ManagedInstanceAlertTypeCredential {
			return input, ErrInvalidInstance
		}
		types[value] = true
	}
	input.AlertTypes = input.AlertTypes[:0]
	for _, value := range []string{model.ManagedInstanceAlertTypeAvailability, model.ManagedInstanceAlertTypeCredential} {
		if types[value] {
			input.AlertTypes = append(input.AlertTypes, value)
		}
	}
	if len(input.AlertTypes) == 0 {
		return input, ErrInvalidInstance
	}
	seenRecipients := map[string]bool{}
	resultRecipients := make([]string, 0, len(input.Recipients))
	for _, value := range input.Recipients {
		value = strings.TrimSpace(value)
		address, err := mail.ParseAddress(value)
		if err != nil || !strings.EqualFold(address.Address, value) {
			return input, ErrInvalidInstance
		}
		key := strings.ToLower(value)
		if !seenRecipients[key] {
			seenRecipients[key] = true
			resultRecipients = append(resultRecipients, value)
		}
	}
	input.Recipients = resultRecipients
	var count int64
	if err := model.DB.Model(&model.ManagedInstance{}).Where("id IN ?", input.InstanceIDs).Count(&count).Error; err != nil || count != int64(len(input.InstanceIDs)) {
		return input, ErrInvalidInstance
	}
	return input, nil
}

func alertRuleModel(input AlertRuleInput, actorID int) model.ManagedInstanceAlertRule {
	types, _ := json.Marshal(input.AlertTypes)
	return model.ManagedInstanceAlertRule{
		Name: input.Name, Description: input.Description, Enabled: input.Enabled,
		AlertTypes: string(types), CheckIntervalSeconds: input.CheckIntervalSeconds,
		FailureThreshold: input.FailureThreshold, Recipients: strings.Join(input.Recipients, ","),
		CreatedBy: actorID, UpdatedBy: actorID,
	}
}

func replaceAlertRuleScope(tx *gorm.DB, ruleID int64, instanceIDs []int64) error {
	if err := tx.Where("rule_id = ?", ruleID).Delete(&model.ManagedInstanceAlertRuleInstance{}).Error; err != nil {
		return err
	}
	rows := make([]model.ManagedInstanceAlertRuleInstance, 0, len(instanceIDs))
	for _, id := range instanceIDs {
		rows = append(rows, model.ManagedInstanceAlertRuleInstance{RuleID: ruleID, InstanceID: id})
	}
	return tx.Create(&rows).Error
}

func activateAlertRule(tx *gorm.DB, rule *model.ManagedInstanceAlertRule, instanceIDs []int64) error {
	var conflicts []int64
	if err := tx.Model(&model.ManagedInstanceAlertAssignment{}).
		Where("instance_id IN ? AND rule_id <> ?", instanceIDs, rule.ID).Pluck("instance_id", &conflicts).Error; err != nil {
		return err
	}
	if len(conflicts) > 0 {
		sort.Slice(conflicts, func(i, j int) bool { return conflicts[i] < conflicts[j] })
		return &AlertRuleConflictError{InstanceIDs: conflicts}
	}
	now := common.GetTimestamp()
	for _, instanceID := range instanceIDs {
		assignment := model.ManagedInstanceAlertAssignment{InstanceID: instanceID, RuleID: rule.ID, UpdatedAt: now}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "instance_id"}}, DoNothing: true}).Create(&assignment)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var existing model.ManagedInstanceAlertAssignment
			if err := tx.First(&existing, "instance_id = ?", instanceID).Error; err != nil {
				return err
			}
			if existing.RuleID != rule.ID {
				return &AlertRuleConflictError{InstanceIDs: []int64{instanceID}}
			}
			if err := tx.Model(&existing).Update("updated_at", now).Error; err != nil {
				return err
			}
		}
	}
	return tx.Model(&model.ManagedInstance{}).Where("id IN ?", instanceIDs).
		Updates(map[string]any{"check_interval_seconds": rule.CheckIntervalSeconds, "updated_at": now}).Error
}

func cancelAlertRuleNotifications(tx *gorm.DB, ruleID int64, instanceIDs []int64, alertTypes []string, all bool) error {
	query := tx.Model(&model.ManagedInstanceAlert{}).Where("rule_id = ? AND status = ?", ruleID, model.ManagedInstanceAlertStatusOpen)
	if !all {
		if len(instanceIDs) == 0 && len(alertTypes) == 0 {
			return nil
		}
		query = query.Where("instance_id IN ? OR alert_type IN ?", nonEmptyIDs(instanceIDs), nonEmptyStrings(alertTypes))
	}
	return query.Updates(map[string]any{
		"email_status": model.ManagedInstanceAlertEmailCancelled, "email_next_retry_at": 0,
		"recovery_email_status": model.ManagedInstanceAlertEmailCancelled, "recovery_email_next_retry_at": 0,
		"updated_at": common.GetTimestamp(),
	}).Error
}

func activePolicy(tx *gorm.DB, instanceID int64) (*activeAlertPolicy, error) {
	var assignment model.ManagedInstanceAlertAssignment
	if err := tx.First(&assignment, "instance_id = ?", instanceID).Error; err != nil {
		return nil, err
	}
	var rule model.ManagedInstanceAlertRule
	if err := tx.Where("id = ? AND enabled = ?", assignment.RuleID, true).First(&rule).Error; err != nil {
		return nil, err
	}
	var setting model.SMTPSetting
	_ = tx.First(&setting, 1).Error
	threshold := rule.FailureThreshold
	if threshold == 0 {
		threshold = setting.InstanceAlertFailureThreshold
		if threshold < 1 || threshold > 100 {
			threshold = defaultManagedInstanceAlertFailureThreshold
		}
	}
	recipients := splitRecipients(rule.Recipients)
	if len(recipients) == 0 {
		recipients = splitRecipients(setting.AlertRecipients)
	}
	return &activeAlertPolicy{RuleID: rule.ID, RuleName: rule.Name, AlertTypes: stringSet(decodeAlertTypes(rule.AlertTypes)),
		FailureThreshold: threshold, Recipients: recipients, NotificationEnabled: setting.Enabled && len(recipients) > 0}, nil
}

func alertRuleView(tx *gorm.DB, rule *model.ManagedInstanceAlertRule) (*AlertRuleView, error) {
	ids, err := alertRuleInstanceIDs(tx, rule.ID)
	if err != nil {
		return nil, err
	}
	policy, policyErr := activePolicyForRule(tx, rule)
	if policyErr != nil {
		return nil, policyErr
	}
	return &AlertRuleView{ID: rule.ID, Name: rule.Name, Description: rule.Description, Enabled: rule.Enabled,
		AlertTypes: decodeAlertTypes(rule.AlertTypes), CheckIntervalSeconds: rule.CheckIntervalSeconds,
		FailureThreshold: rule.FailureThreshold, EffectiveFailureThreshold: policy.FailureThreshold,
		Recipients: splitRecipients(rule.Recipients), EffectiveRecipients: policy.Recipients, InstanceIDs: ids,
		CreatedBy: rule.CreatedBy, UpdatedBy: rule.UpdatedBy, CreatedAt: rule.CreatedAt, UpdatedAt: rule.UpdatedAt}, nil
}

func activePolicyForRule(tx *gorm.DB, rule *model.ManagedInstanceAlertRule) (*activeAlertPolicy, error) {
	var setting model.SMTPSetting
	_ = tx.First(&setting, 1).Error
	threshold := rule.FailureThreshold
	if threshold == 0 {
		threshold = setting.InstanceAlertFailureThreshold
		if threshold < 1 || threshold > 100 {
			threshold = defaultManagedInstanceAlertFailureThreshold
		}
	}
	recipients := splitRecipients(rule.Recipients)
	if len(recipients) == 0 {
		recipients = splitRecipients(setting.AlertRecipients)
	}
	return &activeAlertPolicy{FailureThreshold: threshold, Recipients: recipients, NotificationEnabled: setting.Enabled && len(recipients) > 0}, nil
}

func alertRuleInstanceIDs(tx *gorm.DB, ruleID int64) ([]int64, error) {
	var ids []int64
	err := tx.Model(&model.ManagedInstanceAlertRuleInstance{}).Where("rule_id = ?", ruleID).Order("instance_id ASC").Pluck("instance_id", &ids).Error
	return ids, err
}

func decodeAlertTypes(raw string) []string {
	var values []string
	_ = json.Unmarshal([]byte(raw), &values)
	return values
}
func splitRecipients(raw string) []string {
	result := []string{}
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
func stringSet(values []string) map[string]bool {
	result := map[string]bool{}
	for _, value := range values {
		result[value] = true
	}
	return result
}
func uniquePositiveIDs(values []int64) []int64 {
	seen := map[int64]bool{}
	result := []int64{}
	for _, value := range values {
		if value > 0 && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
func differenceInt64(left, right []int64) []int64 {
	set := map[int64]bool{}
	for _, v := range right {
		set[v] = true
	}
	result := []int64{}
	for _, v := range left {
		if !set[v] {
			result = append(result, v)
		}
	}
	return result
}
func differenceStrings(left, right []string) []string {
	set := stringSet(right)
	result := []string{}
	for _, v := range left {
		if !set[v] {
			result = append(result, v)
		}
	}
	return result
}
func nonEmptyIDs(values []int64) []int64 {
	if len(values) == 0 {
		return []int64{-1}
	}
	return values
}
func nonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return []string{"__none__"}
	}
	return values
}
