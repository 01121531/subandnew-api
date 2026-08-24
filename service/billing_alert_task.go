package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/billingalert"
	"github.com/01121531/subandnew-api/service/metricalert"
)

type billingAlertScanHandler struct{}
type billingAlertEvaluateHandler struct{}
type billingEmailDeliveryHandler struct{}
type exchangeRateRefreshHandler struct{}
type billingAlertExportHandler struct{}
type metricAlertScanHandler struct{}
type metricAlertEvaluateHandler struct{}

type billingEvaluatePayload struct {
	RuleID     int64 `json:"rule_id"`
	InstanceID int64 `json:"instance_id"`
}

type metricEvaluatePayload struct {
	RuleID int64 `json:"rule_id"`
}

type billingEmailPayload struct {
	DeliveryID int64 `json:"delivery_id"`
}

type billingAlertExportPayload struct {
	ExportID int64 `json:"export_id"`
}

func init() {
	RegisterSystemTaskHandler(billingAlertScanHandler{})
	RegisterSystemTaskHandler(billingAlertEvaluateHandler{})
	RegisterSystemTaskHandler(billingEmailDeliveryHandler{})
	RegisterSystemTaskHandler(exchangeRateRefreshHandler{})
	RegisterSystemTaskHandler(billingAlertExportHandler{})
	RegisterSystemTaskHandler(metricAlertScanHandler{})
	RegisterSystemTaskHandler(metricAlertEvaluateHandler{})
}

func (metricAlertScanHandler) Type() string            { return model.SystemTaskTypeMetricAlertScan }
func (metricAlertScanHandler) Enabled() bool           { return true }
func (metricAlertScanHandler) Interval() time.Duration { return 10 * time.Second }
func (metricAlertScanHandler) NewPayload() any         { return nil }

func (metricAlertScanHandler) Run(_ context.Context, task *model.SystemTask, runnerID string) {
	now := common.GetTimestamp()
	rules, err := metricalert.DueRules(now, 500)
	if err == nil {
		for _, rule := range rules {
			_, _, enqueueErr := EnqueueMetricAlertEvaluation(rule.ID)
			if enqueueErr != nil && err == nil {
				err = enqueueErr
			}
			_ = metricalert.TouchNextRun(rule.ID, now, rule.EvaluationIntervalSeconds)
		}
	}
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil {
		status, message = model.SystemTaskStatusFailed, err.Error()
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, status, map[string]any{"rules": len(rules)}, message)
}

func (metricAlertEvaluateHandler) Type() string { return model.SystemTaskTypeMetricAlertEvaluate }

func (metricAlertEvaluateHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := metricEvaluatePayload{}
	err := task.DecodePayload(&payload)
	var result any
	if err == nil && payload.RuleID > 0 {
		result, err = metricalert.EvaluateRule(ctx, payload.RuleID)
	}
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil && !errors.Is(err, metricalert.ErrDataUnavailable) {
		status, message = model.SystemTaskStatusFailed, err.Error()
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, status, result, message)
	_ = enqueueDueBillingDeliveries()
}

func EnqueueMetricAlertEvaluation(ruleID int64) (*model.SystemTask, bool, error) {
	if ruleID <= 0 {
		return nil, false, metricalert.ErrInvalidInput
	}
	return EnqueueScopedSystemTask(model.SystemTaskTypeMetricAlertEvaluate, strconv.FormatInt(ruleID, 10), metricEvaluatePayload{RuleID: ruleID}, nil)
}

func EnqueueBillingAlertExport(filter billingalert.AlertRecordFilter, actorID int) (*model.BillingAlertExport, error) {
	if actorID <= 0 {
		return nil, billingalert.ErrInvalidBillingInput
	}
	encoded, err := json.Marshal(filter)
	if err != nil {
		return nil, err
	}
	task, err := model.CreateQueuedSystemTask(model.SystemTaskTypeBillingAlertExport, nil, nil)
	if err != nil {
		return nil, err
	}
	record := &model.BillingAlertExport{
		TaskID: task.TaskID, ActorID: actorID, Query: string(encoded), Status: "pending",
	}
	if err := model.DB.Create(record).Error; err != nil {
		_ = model.DB.Delete(task).Error
		return nil, err
	}
	payload, _ := json.Marshal(billingAlertExportPayload{ExportID: record.ID})
	if err := model.DB.Model(task).Update("payload", string(payload)).Error; err != nil {
		return nil, err
	}
	notifySystemTaskRunner()
	return record, nil
}

func (billingAlertExportHandler) Type() string { return model.SystemTaskTypeBillingAlertExport }

func (billingAlertExportHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	var payload billingAlertExportPayload
	err := task.DecodePayload(&payload)
	if err == nil {
		err = billingalert.RunAlertExport(ctx, payload.ExportID)
	}
	status := model.SystemTaskStatusSucceeded
	errorMessage := ""
	if err != nil {
		status, errorMessage = model.SystemTaskStatusFailed, err.Error()
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, status, map[string]any{"export_id": payload.ExportID}, errorMessage)
}

func (billingAlertScanHandler) Type() string            { return model.SystemTaskTypeBillingAlertScan }
func (billingAlertScanHandler) Enabled() bool           { return true }
func (billingAlertScanHandler) Interval() time.Duration { return time.Minute }
func (billingAlertScanHandler) NewPayload() any         { return nil }

func (billingAlertScanHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	now := common.GetTimestamp()
	bindings, err := billingalert.DueRuleBindings(now, 500)
	if err == nil {
		for _, binding := range bindings {
			scope := fmt.Sprintf("%d:%d", binding.RuleID, binding.InstanceID)
			_, _, enqueueErr := EnqueueScopedSystemTask(model.SystemTaskTypeBillingAlertEvaluate, scope, billingEvaluatePayload{
				RuleID: binding.RuleID, InstanceID: binding.InstanceID,
			}, nil)
			if enqueueErr != nil && err == nil {
				err = enqueueErr
			}
		}
	}
	if deliveryErr := enqueueDueBillingDeliveries(); deliveryErr != nil && err == nil {
		err = deliveryErr
	}
	if resumeErr := resumeInterruptedBillingAlertExports(); resumeErr != nil && err == nil {
		err = resumeErr
	}
	status := model.SystemTaskStatusSucceeded
	errorMessage := ""
	if err != nil {
		status, errorMessage = model.SystemTaskStatusFailed, err.Error()
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, status, map[string]any{"scanned_at": now}, errorMessage)
}

func resumeInterruptedBillingAlertExports() error {
	var records []*model.BillingAlertExport
	if err := model.DB.Where("status = ?", "running").Find(&records).Error; err != nil {
		return err
	}
	for _, record := range records {
		task, err := model.GetSystemTaskByTaskID(record.TaskID)
		if err == nil && (task.Status == model.SystemTaskStatusPending || task.Status == model.SystemTaskStatusRunning) {
			continue
		}
		newTask, err := model.CreateQueuedSystemTask(model.SystemTaskTypeBillingAlertExport, nil, nil)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(billingAlertExportPayload{ExportID: record.ID})
		if err := model.DB.Model(newTask).Update("payload", string(payload)).Error; err != nil {
			return err
		}
		if err := model.DB.Model(record).Updates(map[string]any{
			"task_id": newTask.TaskID, "status": "pending", "started_at": 0,
			"error_code": "", "updated_at": common.GetTimestamp(),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (billingAlertEvaluateHandler) Type() string { return model.SystemTaskTypeBillingAlertEvaluate }

func (billingAlertEvaluateHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	var payload billingEvaluatePayload
	err := task.DecodePayload(&payload)
	var result any
	if err == nil {
		result, err = billingalert.EvaluateRuleInstance(ctx, payload.RuleID, payload.InstanceID, time.Now())
	}
	status := model.SystemTaskStatusSucceeded
	errorMessage := ""
	if err != nil {
		status, errorMessage = model.SystemTaskStatusFailed, err.Error()
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage)
	_ = enqueueDueBillingDeliveries()
}

func EnqueueBillingRuleEvaluation(ruleID int64, instanceID int64) (*model.SystemTask, bool, error) {
	if ruleID <= 0 || instanceID <= 0 {
		return nil, false, billingalert.ErrInvalidBillingInput
	}
	scope := fmt.Sprintf("%d:%d", ruleID, instanceID)
	return EnqueueScopedSystemTask(model.SystemTaskTypeBillingAlertEvaluate, scope, billingEvaluatePayload{
		RuleID: ruleID, InstanceID: instanceID,
	}, nil)
}

func (billingEmailDeliveryHandler) Type() string { return model.SystemTaskTypeBillingEmailDelivery }

func (billingEmailDeliveryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	var payload billingEmailPayload
	err := task.DecodePayload(&payload)
	if err == nil {
		err = sendBillingDelivery(ctx, payload.DeliveryID)
	}
	status := model.SystemTaskStatusSucceeded
	errorMessage := ""
	if err != nil {
		status, errorMessage = model.SystemTaskStatusFailed, err.Error()
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, status, map[string]any{"delivery_id": payload.DeliveryID}, errorMessage)
}

func (exchangeRateRefreshHandler) Type() string { return model.SystemTaskTypeExchangeRateRefresh }
func (exchangeRateRefreshHandler) Enabled() bool {
	setting, err := billingalert.EnsureExchangeRateSetting()
	return err == nil && setting.Automatic
}
func (exchangeRateRefreshHandler) Interval() time.Duration { return 15 * time.Minute }
func (exchangeRateRefreshHandler) NewPayload() any         { return nil }

func (exchangeRateRefreshHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	var result any
	var err error
	if exchangeRateIsDue(time.Now()) {
		result, err = billingalert.RefreshExchangeRate(ctx, 0)
	}
	status := model.SystemTaskStatusSucceeded
	errorMessage := ""
	if err != nil {
		status, errorMessage = model.SystemTaskStatusFailed, err.Error()
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, status, result, errorMessage)
}

func exchangeRateIsDue(now time.Time) bool {
	setting, err := billingalert.EnsureExchangeRateSetting()
	if err != nil || !setting.Automatic {
		return false
	}
	location, err := time.LoadLocation(setting.Timezone)
	if err != nil {
		return false
	}
	var configured []string
	if json.Unmarshal([]byte(setting.UpdateTimes), &configured) != nil {
		return false
	}
	localNow := now.In(location)
	for _, value := range configured {
		parsed, err := time.Parse("15:04", value)
		if err != nil {
			continue
		}
		due := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), parsed.Hour(), parsed.Minute(), 0, 0, location)
		if !localNow.Before(due) && (setting.LastAttemptAt == 0 || time.Unix(setting.LastAttemptAt, 0).In(location).Before(due)) {
			return true
		}
	}
	return false
}

func enqueueDueBillingDeliveries() error {
	now := common.GetTimestamp()
	var deliveries []*model.BillingEmailDelivery
	if err := model.DB.Where("status IN ? AND next_retry_at <= ?", []string{"pending", "retrying"}, now).
		Order("next_retry_at ASC, id ASC").Limit(200).Find(&deliveries).Error; err != nil {
		return err
	}
	for _, delivery := range deliveries {
		scope := fmt.Sprintf("%d", delivery.ID)
		if _, _, err := EnqueueScopedSystemTask(model.SystemTaskTypeBillingEmailDelivery, scope, billingEmailPayload{DeliveryID: delivery.ID}, nil); err != nil {
			return err
		}
	}
	return nil
}

func sendBillingDelivery(ctx context.Context, deliveryID int64) error {
	var delivery model.BillingEmailDelivery
	if err := model.DB.First(&delivery, deliveryID).Error; err != nil {
		return err
	}
	if delivery.Status == "sent" || delivery.Status == "failed" {
		return nil
	}
	var event model.BillingAlertEvent
	if err := model.DB.First(&event, delivery.EventID).Error; err != nil {
		return err
	}
	var rule model.BillingAlertRule
	_ = model.DB.First(&rule, event.RuleID).Error
	var instance model.ManagedInstance
	_ = model.DB.First(&instance, event.InstanceID).Error
	subject, textBody, htmlBody := billingEmailContent(&event, &rule, &instance)
	err := billingalert.SendSMTPMessage(ctx, billingalert.SMTPMessage{
		Recipients: []string{delivery.Recipient}, Subject: subject, TextBody: textBody, HTMLBody: htmlBody,
	})
	now := common.GetTimestamp()
	attempts := delivery.Attempts + 1
	if err == nil {
		return model.DB.Model(&delivery).Updates(map[string]any{
			"status": "sent", "attempts": attempts, "last_error": "", "sent_at": now, "updated_at": now,
		}).Error
	}
	status := "retrying"
	nextRetry := now + int64((time.Minute * time.Duration(1<<min(attempts-1, 5))).Seconds())
	if attempts >= 6 {
		status, nextRetry = "failed", 0
	}
	_ = model.DB.Model(&delivery).Updates(map[string]any{
		"status": status, "attempts": attempts, "last_error": err.Error(),
		"next_retry_at": nextRetry, "updated_at": now,
	}).Error
	return err
}

func billingEmailContent(event *model.BillingAlertEvent, rule *model.BillingAlertRule, instance *model.ManagedInstance) (string, string, string) {
	if event.SourceType == model.AlertSourceMetric {
		return metricAlertEmailContent(event)
	}
	ruleName := event.RuleName
	if ruleName == "" {
		ruleName = rule.Name
	}
	instanceName := event.InstanceName
	if instanceName == "" {
		instanceName = instance.Name
	}
	instanceKind := event.InstanceKind
	if instanceKind == "" {
		instanceKind = instance.Kind
	}
	title := "账单预警"
	switch event.EventType {
	case model.BillingAlertEventFailure:
		title = "账单监控异常"
	case model.BillingAlertEventRecovery:
		title = "账单监控已恢复"
	}
	subject := fmt.Sprintf("[%s] %s · %s", title, instanceName, ruleName)
	lines := []string{
		"实例：" + instanceName,
		"系统类型：" + instanceKind,
		"规则：" + ruleName,
		"美元消耗：$" + event.USDTotal,
		"人民币账单：¥" + event.CNYTotal,
		"折扣比例：" + billingDiscountDisplay(event.DiscountRate),
		"USD/CNY 汇率：" + event.ExchangeRate,
	}
	if event.ErrorCode != "" {
		lines = append(lines, "错误代码："+event.ErrorCode)
	}
	textBody := strings.Join(lines, "\n")
	var rows strings.Builder
	for _, line := range lines {
		parts := strings.SplitN(line, "：", 2)
		if len(parts) == 2 {
			rows.WriteString("<tr><th style=\"text-align:left;padding:8px;color:#6b7280\">" + html.EscapeString(parts[0]) + "</th><td style=\"padding:8px\">" + html.EscapeString(parts[1]) + "</td></tr>")
		}
	}
	htmlBody := "<div style=\"font-family:Arial,sans-serif;max-width:640px\"><h2>" + html.EscapeString(title) + "</h2><table style=\"border-collapse:collapse;width:100%\">" + rows.String() + "</table></div>"
	return subject, textBody, htmlBody
}

func metricAlertEmailContent(event *model.BillingAlertEvent) (string, string, string) {
	title := "指标预警"
	switch event.EventType {
	case model.MetricAlertEventRecovered:
		title = "指标预警已恢复"
	case model.MetricAlertEventMonitorFailure:
		title = "指标监控异常"
	case model.MetricAlertEventMonitorRecovery:
		title = "指标监控已恢复"
	}
	instanceName := event.InstanceName
	if instanceName == "" {
		instanceName = "汇总范围"
	}
	subject := fmt.Sprintf("[%s] %s · %s", title, instanceName, event.RuleName)
	lines := []string{
		"范围：" + instanceName,
		"规则：" + event.RuleName,
		"计算模式：" + metricScopeDisplay(event.ScopeMode),
	}
	if event.ThresholdName != "" {
		lines = append(lines, "指标："+event.ThresholdName)
	}
	if event.Threshold != "" {
		lines = append(lines, "触发条件："+event.Threshold)
	}
	if event.ObservedValues != "" {
		var values map[string]float64
		if json.Unmarshal([]byte(event.ObservedValues), &values) == nil {
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				lines = append(lines, metricEmailLabel(key)+"："+strconv.FormatFloat(values[key], 'f', -1, 64))
			}
		}
	}
	if event.ErrorCode != "" {
		lines = append(lines, "错误代码："+event.ErrorCode)
	}
	textBody := strings.Join(lines, "\n")
	var rows strings.Builder
	for _, line := range lines {
		parts := strings.SplitN(line, "：", 2)
		if len(parts) == 2 {
			rows.WriteString("<tr><th style=\"text-align:left;padding:8px;color:#6b7280\">" + html.EscapeString(parts[0]) + "</th><td style=\"padding:8px\">" + html.EscapeString(parts[1]) + "</td></tr>")
		}
	}
	htmlBody := "<div style=\"font-family:Arial,sans-serif;max-width:640px\"><h2>" + html.EscapeString(title) + "</h2><table style=\"border-collapse:collapse;width:100%\">" + rows.String() + "</table></div>"
	return subject, textBody, htmlBody
}

func metricScopeDisplay(value string) string {
	if value == metricalert.ScopeAggregate {
		return "实例汇总"
	}
	return "每个实例独立"
}

func metricEmailLabel(key string) string {
	for _, definition := range metricalert.MetricDefinitions() {
		if definition.Key == key {
			return definition.Label
		}
	}
	return key
}

func billingDiscountDisplay(value string) string {
	formatted, err := billingalert.FormatDiscountPercent(value)
	if err != nil {
		return value
	}
	return formatted
}
