package managedinstance

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const managedInstanceAlertProjectionBatchSize = 500

type AlertProjectionRepairResult struct {
	Processed int `json:"processed"`
}

// SyncAlertEvents maintains the read-optimized events used by the unified
// alert record list. Email delivery remains owned by ManagedInstanceAlert.
func SyncAlertEvents(tx *gorm.DB, instance *model.ManagedInstance, alert *model.ManagedInstanceAlert) error {
	if tx == nil || alert == nil || alert.Id <= 0 {
		return ErrInvalidInstance
	}
	if instance == nil || instance.Id != alert.InstanceId {
		var loaded model.ManagedInstance
		if err := tx.Select("id", "name", "kind").First(&loaded, alert.InstanceId).Error; err != nil {
			return err
		}
		instance = &loaded
	}
	if err := upsertInstanceAlertEvent(tx, instance, alert, false); err != nil {
		return err
	}
	if alert.Status == model.ManagedInstanceAlertStatusResolved && alert.ResolvedAt > 0 {
		return upsertInstanceAlertEvent(tx, instance, alert, true)
	}
	return nil
}

func upsertInstanceAlertEvent(tx *gorm.DB, instance *model.ManagedInstance, alert *model.ManagedInstanceAlert, recovery bool) error {
	eventType := model.InstanceAlertEventFailure
	eventPhase := "failure"
	createdAt := alert.FirstSeenAt
	recipients := alert.EmailRecipients
	if recovery {
		eventType = model.InstanceAlertEventRecovered
		eventPhase = "recovery"
		createdAt = alert.ResolvedAt
		recipients = alert.RecoveryEmailRecipients
		if recipients == "" {
			recipients = alert.EmailRecipients
		}
	}
	if createdAt <= 0 {
		createdAt = alert.LastSeenAt
	}
	conditionBytes, _ := json.Marshal(map[string]any{
		"alert_type": alert.AlertType,
		"phase":      eventPhase,
	})
	valueBytes, _ := json.Marshal(map[string]any{
		"occurrences": alert.Occurrences,
		"status":      alert.Status,
	})
	thresholdName := "实例不可用"
	if alert.AlertType == model.ManagedInstanceAlertTypeCredential {
		thresholdName = "实例凭据异常"
	}
	event := &model.BillingAlertEvent{
		EventKey:       fmt.Sprintf("instance-alert:%d:%s", alert.Id, eventPhase),
		EventType:      eventType,
		SourceType:     model.AlertSourceInstance,
		SourceRecordID: alert.Id,
		InstanceID:     alert.InstanceId,
		RuleName:       "实例巡检",
		InstanceName:   instance.Name,
		InstanceKind:   instance.Kind,
		ThresholdName:  thresholdName,
		Timezone:       "Asia/Shanghai",
		Recipients:     recipients,
		ErrorCode:      alert.ErrorCode,
		ScopeMode:      "per_instance",
		MetricKey:      "instance_connected",
		Conditions:     string(conditionBytes),
		ObservedValues: string(valueBytes),
		CreatedAt:      createdAt,
	}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "event_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_type", "source_record_id", "instance_id", "rule_name",
			"instance_name", "instance_kind", "threshold_name",
			"timezone", "recipients", "error_code", "scope_mode", "metric_key",
			"conditions", "observed_values", "created_at",
		}),
	}).Create(event).Error
}

// RepairAlertEventProjections is idempotent and makes pre-upgrade alerts
// immediately visible in the unified record list and exports.
func RepairAlertEventProjections() (*AlertProjectionRepairResult, error) {
	result := &AlertProjectionRepairResult{}
	if model.DB == nil {
		return result, errors.New("database is not initialized")
	}
	var lastID int64
	for {
		var alerts []model.ManagedInstanceAlert
		if err := model.DB.Where("id > ?", lastID).Order("id ASC").Limit(managedInstanceAlertProjectionBatchSize).Find(&alerts).Error; err != nil {
			return result, err
		}
		if len(alerts) == 0 {
			return result, nil
		}
		for index := range alerts {
			alert := &alerts[index]
			if err := model.DB.Transaction(func(tx *gorm.DB) error {
				return SyncAlertEvents(tx, nil, alert)
			}); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					lastID = alert.Id
					continue
				}
				return result, err
			}
			result.Processed++
			lastID = alert.Id
		}
		if len(alerts) < managedInstanceAlertProjectionBatchSize {
			return result, nil
		}
	}
}
