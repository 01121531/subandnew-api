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
	RuleName      string               `json:"rule_name"`
	InstanceName  string               `json:"instance_name"`
	InstanceKind  string               `json:"instance_kind"`
	ThresholdName string               `json:"threshold_name"`
	Deliveries    []*AlertDeliveryView `json:"deliveries"`
}

type AlertDeliveryView struct {
	ID          int64  `json:"id"`
	Phase       string `json:"phase,omitempty"`
	Recipient   string `json:"recipient"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	LastError   string `json:"last_error"`
	NextRetryAt int64  `json:"next_retry_at"`
	SentAt      int64  `json:"sent_at"`
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
	} else if event.SourceType == model.AlertSourceBilling {
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
	if event.SourceType == model.AlertSourceBilling && event.ThresholdID > 0 {
		var threshold model.BillingAlertThreshold
		if err := model.DB.Select("name").First(&threshold, event.ThresholdID).Error; err == nil {
			view.ThresholdName = threshold.Name
		}
	}
	if event.SourceType == model.AlertSourceInstance {
		view.Deliveries = instanceAlertDeliveries(event)
	} else {
		var deliveries []*model.BillingEmailDelivery
		if err := model.DB.Where("event_id = ?", event.ID).Order("id ASC").Find(&deliveries).Error; err != nil {
			return nil, err
		}
		view.Deliveries = make([]*AlertDeliveryView, 0, len(deliveries))
		for _, delivery := range deliveries {
			view.Deliveries = append(view.Deliveries, &AlertDeliveryView{
				ID: delivery.ID, Recipient: delivery.Recipient, Status: delivery.Status,
				Attempts: delivery.Attempts, LastError: delivery.LastError,
				NextRetryAt: delivery.NextRetryAt, SentAt: delivery.SentAt,
			})
		}
	}
	return view, nil
}

func instanceAlertDeliveries(event *model.BillingAlertEvent) []*AlertDeliveryView {
	if event.SourceRecordID <= 0 {
		return []*AlertDeliveryView{}
	}
	var alert model.ManagedInstanceAlert
	if err := model.DB.First(&alert, event.SourceRecordID).Error; err != nil {
		return []*AlertDeliveryView{}
	}
	phase := "failure"
	recipients := alert.EmailRecipients
	status := alert.EmailStatus
	attempts := alert.EmailAttempts
	lastError := alert.EmailError
	nextRetryAt := alert.EmailNextRetryAt
	sentAt := alert.EmailSentAt
	if event.EventType == model.InstanceAlertEventRecovered {
		phase = "recovery"
		recipients = alert.RecoveryEmailRecipients
		status = alert.RecoveryEmailStatus
		attempts = alert.RecoveryEmailAttempts
		lastError = alert.RecoveryEmailError
		nextRetryAt = alert.RecoveryEmailNextRetryAt
		sentAt = alert.RecoveryEmailSentAt
	}
	values := strings.Split(recipients, ",")
	result := make([]*AlertDeliveryView, 0, len(values))
	for index, recipient := range values {
		recipient = strings.TrimSpace(recipient)
		if recipient == "" {
			continue
		}
		result = append(result, &AlertDeliveryView{
			ID: -(event.ID*1000 + int64(index) + 1), Phase: phase, Recipient: recipient,
			Status: status, Attempts: attempts, LastError: lastError,
			NextRetryAt: nextRetryAt, SentAt: sentAt,
		})
	}
	if len(result) == 0 && status != "" {
		result = append(result, &AlertDeliveryView{
			ID: -(event.ID*1000 + 1), Phase: phase, Status: status, Attempts: attempts,
			LastError: lastError, NextRetryAt: nextRetryAt, SentAt: sentAt,
		})
	}
	return result
}
