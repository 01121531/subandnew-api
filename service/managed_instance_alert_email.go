package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/logger"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/billingalert"
)

type managedInstanceAlertEmailPayload struct {
	AlertID int64 `json:"alert_id"`
}

type managedInstanceAlertEmailHandler struct{}

func init() {
	RegisterSystemTaskHandler(managedInstanceAlertEmailHandler{})
}

func (managedInstanceAlertEmailHandler) Type() string {
	return model.SystemTaskTypeManagedInstanceAlertEmail
}

func (managedInstanceAlertEmailHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := managedInstanceAlertEmailPayload{}
	if err := task.DecodePayload(&payload); err != nil || payload.AlertID <= 0 || task.ScopeKey != strconv.FormatInt(payload.AlertID, 10) {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "invalid_alert_email_payload")
		return
	}
	if err := sendManagedInstanceAlertEmail(ctx, payload.AlertID); err != nil {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "alert_email_failed")
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, map[string]any{"alert_id": payload.AlertID}, "")
}

func enqueueDueManagedInstanceAlertEmails(now int64) {
	var alerts []model.ManagedInstanceAlert
	err := model.DB.Where(
		"status = ? AND email_status IN ? AND email_next_retry_at <= ?",
		model.ManagedInstanceAlertStatusOpen,
		[]string{"", model.ManagedInstanceAlertEmailPending, model.ManagedInstanceAlertEmailRetrying},
		now,
	).Order("first_seen_at asc, id asc").Limit(100).Find(&alerts).Error
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("managed instance alert email query failed: %v", err))
		return
	}
	for _, alert := range alerts {
		if _, _, err := EnqueueScopedSystemTask(
			model.SystemTaskTypeManagedInstanceAlertEmail,
			strconv.FormatInt(alert.Id, 10),
			managedInstanceAlertEmailPayload{AlertID: alert.Id},
			nil,
		); err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed instance alert email enqueue failed: alert=%d err=%v", alert.Id, err))
		}
	}
}

func sendManagedInstanceAlertEmail(ctx context.Context, alertID int64) error {
	var alert model.ManagedInstanceAlert
	if err := model.DB.First(&alert, alertID).Error; err != nil {
		return err
	}
	if alert.Status != model.ManagedInstanceAlertStatusOpen {
		return model.DB.Model(&alert).Updates(map[string]any{
			"email_status": model.ManagedInstanceAlertEmailCancelled, "email_next_retry_at": 0,
		}).Error
	}
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, alert.InstanceId).Error; err != nil {
		return recordManagedInstanceAlertEmailFailure(&alert, err)
	}
	recipients, err := billingalert.ManagedInstanceAlertRecipients()
	if err == nil {
		subject, textBody, htmlBody := managedInstanceAlertEmailContent(&instance, &alert)
		err = billingalert.SendSMTPMessage(ctx, billingalert.SMTPMessage{
			Recipients: recipients, Subject: subject, TextBody: textBody, HTMLBody: htmlBody,
		})
	}
	if err != nil {
		return recordManagedInstanceAlertEmailFailure(&alert, err)
	}
	now := common.GetTimestamp()
	return model.DB.Model(&alert).Updates(map[string]any{
		"email_status": model.ManagedInstanceAlertEmailSent, "email_recipients": strings.Join(recipients, ","),
		"email_attempts": alert.EmailAttempts + 1, "email_error": "", "email_sent_at": now,
		"email_next_retry_at": 0, "updated_at": now,
	}).Error
}

func recordManagedInstanceAlertEmailFailure(alert *model.ManagedInstanceAlert, deliveryErr error) error {
	if alert == nil {
		return deliveryErr
	}
	attempts := alert.EmailAttempts + 1
	delay := time.Minute * time.Duration(1<<min(attempts-1, 6))
	if delay > time.Hour {
		delay = time.Hour
	}
	now := common.GetTimestamp()
	if err := model.DB.Model(alert).Updates(map[string]any{
		"email_status": model.ManagedInstanceAlertEmailRetrying, "email_attempts": attempts,
		"email_error": deliveryErr.Error(), "email_next_retry_at": now + int64(delay/time.Second), "updated_at": now,
	}).Error; err != nil {
		return errors.Join(deliveryErr, err)
	}
	return deliveryErr
}

func managedInstanceAlertEmailContent(instance *model.ManagedInstance, alert *model.ManagedInstanceAlert) (string, string, string) {
	checkedAt := time.Unix(alert.LastSeenAt, 0).In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02 15:04:05")
	alertName := "实例不可用"
	if alert.AlertType == model.ManagedInstanceAlertTypeCredential {
		alertName = "实例凭据异常"
	}
	subject := fmt.Sprintf("[%s] %s", alertName, instance.Name)
	lines := []string{
		"实例：" + instance.Name,
		"系统类型：" + instance.Kind,
		"站点地址：" + instance.BaseURL,
		"错误代码：" + alert.ErrorCode,
		"连续失败：" + strconv.Itoa(instance.ConsecutiveFailures),
		"巡检时间：" + checkedAt + " (Asia/Shanghai)",
	}
	textBody := strings.Join(lines, "\n")
	var rows strings.Builder
	for _, line := range lines {
		parts := strings.SplitN(line, "：", 2)
		if len(parts) != 2 {
			continue
		}
		rows.WriteString("<tr><th style=\"padding:6px 12px;text-align:left;color:#64748b\">")
		rows.WriteString(html.EscapeString(parts[0]))
		rows.WriteString("</th><td style=\"padding:6px 12px\">")
		rows.WriteString(html.EscapeString(parts[1]))
		rows.WriteString("</td></tr>")
	}
	htmlBody := "<h2>" + html.EscapeString(alertName) + "</h2><table>" + rows.String() + "</table>"
	return subject, textBody, htmlBody
}
