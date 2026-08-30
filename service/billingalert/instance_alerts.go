package billingalert

import (
	"strings"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

type InstanceAlertFilter struct {
	Page           int
	PageSize       int
	InstanceID     int64
	Status         string
	AlertType      string
	DeliveryStatus string
	Search         string
	StartTime      int64
	EndTime        int64
}

type InstanceAlertView struct {
	model.ManagedInstanceAlert
	InstanceName string `json:"instance_name"`
	InstanceKind string `json:"instance_kind"`
}

type InstanceAlertPage struct {
	Items    []*InstanceAlertView `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

func ListInstanceAlerts(filter InstanceAlertFilter) (*InstanceAlertPage, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	query, err := applyInstanceAlertFilter(model.DB.Model(&model.ManagedInstanceAlert{}), filter)
	if err != nil {
		return nil, err
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var alerts []*model.ManagedInstanceAlert
	if err := query.Order("managed_instance_alerts.last_seen_at DESC, managed_instance_alerts.id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&alerts).Error; err != nil {
		return nil, err
	}
	items := make([]*InstanceAlertView, 0, len(alerts))
	for _, alert := range alerts {
		view := &InstanceAlertView{ManagedInstanceAlert: *alert}
		var instance model.ManagedInstance
		if err := model.DB.Select("name", "kind").First(&instance, alert.InstanceId).Error; err == nil {
			view.InstanceName = instance.Name
			view.InstanceKind = instance.Kind
		}
		items = append(items, view)
	}
	return &InstanceAlertPage{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func applyInstanceAlertFilter(query *gorm.DB, filter InstanceAlertFilter) (*gorm.DB, error) {
	if filter.InstanceID > 0 {
		query = query.Where("managed_instance_alerts.instance_id = ?", filter.InstanceID)
	}
	if filter.Status != "" {
		if filter.Status != model.ManagedInstanceAlertStatusOpen && filter.Status != model.ManagedInstanceAlertStatusResolved {
			return nil, ErrInvalidBillingInput
		}
		query = query.Where("managed_instance_alerts.status = ?", filter.Status)
	}
	if filter.AlertType != "" {
		if filter.AlertType != model.ManagedInstanceAlertTypeAvailability && filter.AlertType != model.ManagedInstanceAlertTypeCredential {
			return nil, ErrInvalidBillingInput
		}
		query = query.Where("managed_instance_alerts.alert_type = ?", filter.AlertType)
	}
	if filter.DeliveryStatus != "" {
		valid := map[string]bool{
			model.ManagedInstanceAlertEmailPending: true, model.ManagedInstanceAlertEmailRetrying: true,
			model.ManagedInstanceAlertEmailSent: true, model.ManagedInstanceAlertEmailCancelled: true,
		}
		if !valid[filter.DeliveryStatus] {
			return nil, ErrInvalidBillingInput
		}
		query = query.Where("managed_instance_alerts.email_status = ? OR managed_instance_alerts.recovery_email_status = ?", filter.DeliveryStatus, filter.DeliveryStatus)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + search + "%"
		query = query.Joins("LEFT JOIN managed_instances ON managed_instances.id = managed_instance_alerts.instance_id").Where(
			"managed_instances.name LIKE ? OR managed_instances.kind LIKE ? OR managed_instance_alerts.error_code LIKE ? OR managed_instance_alerts.email_recipients LIKE ? OR managed_instance_alerts.recovery_email_recipients LIKE ?",
			like, like, like, like, like,
		)
	}
	if filter.StartTime > 0 {
		query = query.Where("managed_instance_alerts.last_seen_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("managed_instance_alerts.last_seen_at < ?", filter.EndTime)
	}
	return query, nil
}
