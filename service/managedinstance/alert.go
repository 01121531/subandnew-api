package managedinstance

import (
	"errors"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AlertListFilter struct {
	InstanceID int64
	Status     string
	Page       int
	PageSize   int
}

type AlertListResult struct {
	Items    []*model.ManagedInstanceAlert `json:"items"`
	Total    int64                         `json:"total"`
	Page     int                           `json:"page"`
	PageSize int                           `json:"page_size"`
}

func ListAlerts(filter AlertListFilter) (*AlertListResult, error) {
	if filter.InstanceID < 0 {
		return nil, ErrInvalidInstance
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	query := model.DB.Model(&model.ManagedInstanceAlert{})
	if filter.InstanceID > 0 {
		query = query.Where("instance_id = ?", filter.InstanceID)
	}
	if filter.Status != "" {
		if filter.Status != model.ManagedInstanceAlertStatusOpen && filter.Status != model.ManagedInstanceAlertStatusResolved {
			return nil, ErrInvalidInstance
		}
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var alerts []*model.ManagedInstanceAlert
	if err := query.Order("last_seen_at desc, id desc").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&alerts).Error; err != nil {
		return nil, err
	}
	return &AlertListResult{Items: alerts, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func reconcileProbeFailureAlert(tx *gorm.DB, instance *model.ManagedInstance, status string, errorCode string, checkedAt int64, nextFailures int) error {
	alertType := model.ManagedInstanceAlertTypeAvailability
	thresholdReached := nextFailures >= 3
	if status == model.ManagedInstanceStatusAuthFailed {
		alertType = model.ManagedInstanceAlertTypeCredential
		thresholdReached = true
	}
	if !thresholdReached {
		return nil
	}
	var alert model.ManagedInstanceAlert
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("instance_id = ? AND alert_type = ? AND status = ?", instance.Id, alertType, model.ManagedInstanceAlertStatusOpen).
		Order("id desc").First(&alert).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		alert = model.ManagedInstanceAlert{
			InstanceId: instance.Id, AlertType: alertType, Status: model.ManagedInstanceAlertStatusOpen,
			ErrorCode: errorCode, Occurrences: 1, FirstSeenAt: checkedAt, LastSeenAt: checkedAt,
		}
		if err := tx.Create(&alert).Error; err != nil {
			return err
		}
		return writeAuditOutcome(tx, instance.Id, 0, "alert_open", "succeeded", map[string]any{
			"alert_id": alert.Id, "alert_type": alertType, "error_code": errorCode,
		})
	}
	return tx.Model(&model.ManagedInstanceAlert{}).Where("id = ?", alert.Id).Updates(map[string]any{
		"error_code": errorCode, "occurrences": gorm.Expr("occurrences + 1"), "last_seen_at": checkedAt, "updated_at": checkedAt,
	}).Error
}

func resolveProbeAlerts(tx *gorm.DB, instance *model.ManagedInstance, checkedAt int64) error {
	var alerts []model.ManagedInstanceAlert
	if err := tx.Where("instance_id = ? AND status = ?", instance.Id, model.ManagedInstanceAlertStatusOpen).Find(&alerts).Error; err != nil {
		return err
	}
	for _, alert := range alerts {
		updates := map[string]any{
			"status": model.ManagedInstanceAlertStatusResolved, "resolved_at": checkedAt,
			"last_seen_at": checkedAt, "updated_at": checkedAt,
		}
		if alert.EmailStatus != model.ManagedInstanceAlertEmailSent {
			updates["email_status"] = model.ManagedInstanceAlertEmailCancelled
			updates["email_next_retry_at"] = 0
		}
		if err := tx.Model(&model.ManagedInstanceAlert{}).Where("id = ? AND status = ?", alert.Id, model.ManagedInstanceAlertStatusOpen).Updates(updates).Error; err != nil {
			return err
		}
		if err := writeAuditOutcome(tx, instance.Id, 0, "alert_resolve", "succeeded", map[string]any{
			"alert_id": alert.Id, "alert_type": alert.AlertType,
		}); err != nil {
			return err
		}
	}
	return nil
}
