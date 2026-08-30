package managedinstance

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

type AlertRuleMigrationResult struct {
	Rules     int `json:"rules"`
	Instances int `json:"instances"`
}

// MigrateLegacyAlertRules converts the per-instance legacy settings once.
// New instances receive AlertRuleMigratedAt in BeforeCreate and are therefore
// intentionally not enrolled in a rule automatically.
func MigrateLegacyAlertRules() (*AlertRuleMigrationResult, error) {
	result := &AlertRuleMigrationResult{}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var instances []model.ManagedInstance
		if err := tx.Where("alert_rule_migrated_at = 0").Order("id ASC").Find(&instances).Error; err != nil {
			return err
		}
		groups := map[string][]model.ManagedInstance{}
		for _, instance := range instances {
			key := fmt.Sprintf("%d:%d", instance.CheckIntervalSeconds, instance.AlertFailureThreshold)
			groups[key] = append(groups[key], instance)
		}
		keys := make([]string, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		now := common.GetTimestamp()
		alertTypes, _ := json.Marshal([]string{model.ManagedInstanceAlertTypeAvailability, model.ManagedInstanceAlertTypeCredential})
		for _, key := range keys {
			group := groups[key]
			for start := 0; start < len(group); start += 100 {
				end := start + 100
				if end > len(group) {
					end = len(group)
				}
				chunk := group[start:end]
				rule := model.ManagedInstanceAlertRule{
					Name: fmt.Sprintf("迁移实例预警 %d", result.Rules+1), Description: "由升级前的实例巡检配置自动迁移",
					Enabled: true, AlertTypes: string(alertTypes), CheckIntervalSeconds: chunk[0].CheckIntervalSeconds,
					FailureThreshold: chunk[0].AlertFailureThreshold, CreatedBy: 1, UpdatedBy: 1,
				}
				if err := tx.Create(&rule).Error; err != nil {
					return err
				}
				ids := make([]int64, 0, len(chunk))
				for _, instance := range chunk {
					ids = append(ids, instance.Id)
				}
				if err := replaceAlertRuleScope(tx, rule.ID, ids); err != nil {
					return err
				}
				if err := activateAlertRule(tx, &rule, ids); err != nil {
					return err
				}
				if err := tx.Model(&model.ManagedInstance{}).Where("id IN ?", ids).Update("alert_rule_migrated_at", now).Error; err != nil {
					return err
				}
				if err := tx.Model(&model.ManagedInstanceAlert{}).
					Where("instance_id IN ? AND status = ? AND rule_id = 0", ids, model.ManagedInstanceAlertStatusOpen).
					Updates(map[string]any{"rule_id": rule.ID, "rule_name": rule.Name, "updated_at": now}).Error; err != nil {
					return err
				}
				policy, err := activePolicyForRule(tx, &rule)
				if err != nil {
					return err
				}
				if err := tx.Model(&model.ManagedInstanceAlert{}).
					Where("rule_id = ? AND status = ? AND email_recipients = ''", rule.ID, model.ManagedInstanceAlertStatusOpen).
					Update("email_recipients", strings.Join(policy.Recipients, ",")).Error; err != nil {
					return err
				}
				if !policy.NotificationEnabled {
					if err := tx.Model(&model.ManagedInstanceAlert{}).
						Where("rule_id = ? AND status = ? AND email_status IN ?", rule.ID, model.ManagedInstanceAlertStatusOpen,
							[]string{model.ManagedInstanceAlertEmailPending, model.ManagedInstanceAlertEmailRetrying}).
						Updates(map[string]any{"email_status": model.ManagedInstanceAlertEmailCancelled, "email_next_retry_at": 0}).Error; err != nil {
						return err
					}
				}
				result.Rules++
				result.Instances += len(ids)
			}
		}
		return nil
	})
	return result, err
}
