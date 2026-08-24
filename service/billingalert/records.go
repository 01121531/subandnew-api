package billingalert

import (
	"errors"
	"strings"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

type AlertRecordFilter struct {
	Page       int
	PageSize   int
	InstanceID int64
	RuleID     int64
	EventType  string
	SourceType string
	MetricKey  string
	Currency   string
	Recipient  string
	StartTime  int64
	EndTime    int64
}

type AlertRecordView struct {
	model.BillingAlertEvent
	RuleName      string                        `json:"rule_name"`
	InstanceName  string                        `json:"instance_name"`
	InstanceKind  string                        `json:"instance_kind"`
	ThresholdName string                        `json:"threshold_name"`
	Deliveries    []*model.BillingEmailDelivery `json:"deliveries"`
}

type AlertRecordPage struct {
	Items    []*AlertRecordView `json:"items"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}

func ListAlertRecords(filter AlertRecordFilter) (*AlertRecordPage, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 200 {
		filter.PageSize = 200
	}
	query := applyAlertRecordFilter(model.DB.Model(&model.BillingAlertEvent{}), filter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var events []*model.BillingAlertEvent
	if err := query.Order("created_at DESC, id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&events).Error; err != nil {
		return nil, err
	}
	items := make([]*AlertRecordView, 0, len(events))
	for _, event := range events {
		view, err := alertRecordView(event)
		if err != nil {
			return nil, err
		}
		items = append(items, view)
	}
	return &AlertRecordPage{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func GetAlertRecord(id int64) (*AlertRecordView, error) {
	var event model.BillingAlertEvent
	if err := model.DB.First(&event, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBillingNotFound
		}
		return nil, err
	}
	return alertRecordView(&event)
}

func applyAlertRecordFilter(query *gorm.DB, filter AlertRecordFilter) *gorm.DB {
	if filter.InstanceID > 0 {
		query = query.Where("billing_alert_events.instance_id = ?", filter.InstanceID)
	}
	if filter.RuleID > 0 {
		query = query.Where("billing_alert_events.rule_id = ?", filter.RuleID)
	}
	if filter.EventType != "" {
		query = query.Where("billing_alert_events.event_type = ?", filter.EventType)
	}
	if filter.SourceType != "" {
		query = query.Where("billing_alert_events.source_type = ?", filter.SourceType)
	}
	if filter.MetricKey != "" {
		query = query.Where("billing_alert_events.metric_key = ?", filter.MetricKey)
	}
	if filter.Currency != "" {
		query = query.Where("billing_alert_events.currency = ?", strings.ToUpper(filter.Currency))
	}
	if filter.Recipient != "" {
		query = query.Where("billing_alert_events.recipients LIKE ?", "%"+strings.TrimSpace(filter.Recipient)+"%")
	}
	if filter.StartTime > 0 {
		query = query.Where("billing_alert_events.created_at >= ?", filter.StartTime)
	}
	if filter.EndTime > 0 {
		query = query.Where("billing_alert_events.created_at < ?", filter.EndTime)
	}
	return query
}

func alertRecordView(event *model.BillingAlertEvent) (*AlertRecordView, error) {
	view := &AlertRecordView{
		BillingAlertEvent: *event, RuleName: event.RuleName, InstanceName: event.InstanceName,
		InstanceKind: event.InstanceKind, ThresholdName: event.ThresholdName,
	}
	if event.SourceType == model.AlertSourceMetric {
		var rule model.MetricAlertRule
		if err := model.DB.Select("name").First(&rule, event.RuleID).Error; err == nil {
			view.RuleName = rule.Name
		}
	} else {
		var rule model.BillingAlertRule
		if err := model.DB.Select("name").First(&rule, event.RuleID).Error; err == nil {
			view.RuleName = rule.Name
		}
	}
	var instance model.ManagedInstance
	if err := model.DB.Select("name", "kind").First(&instance, event.InstanceID).Error; err == nil {
		view.InstanceName = instance.Name
		view.InstanceKind = instance.Kind
	}
	if event.SourceType != model.AlertSourceMetric && event.ThresholdID > 0 {
		var threshold model.BillingAlertThreshold
		if err := model.DB.Select("name").First(&threshold, event.ThresholdID).Error; err == nil {
			view.ThresholdName = threshold.Name
		}
	}
	if err := model.DB.Where("event_id = ?", event.ID).Order("id ASC").Find(&view.Deliveries).Error; err != nil {
		return nil, err
	}
	return view, nil
}
