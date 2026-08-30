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
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	managedInstanceAlertEmailPhaseFailure  = "failure"
	managedInstanceAlertEmailPhaseRecovery = "recovery"
)

type managedInstanceAlertEmailPayload struct {
	AlertID int64  `json:"alert_id"`
	Phase   string `json:"phase,omitempty"`
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
	if err := task.DecodePayload(&payload); err != nil || payload.AlertID <= 0 {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "invalid_alert_email_payload")
		return
	}
	if payload.Phase == "" {
		payload.Phase = managedInstanceAlertEmailPhaseFailure
	}
	if task.ScopeKey != managedInstanceAlertEmailScope(payload.AlertID, payload.Phase) {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "invalid_alert_email_payload")
		return
	}
	if err := sendManagedInstanceAlertEmailPhase(ctx, payload.AlertID, payload.Phase); err != nil {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "alert_email_failed")
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, map[string]any{
		"alert_id": payload.AlertID, "phase": payload.Phase,
	}, "")
}

func managedInstanceAlertEmailScope(alertID int64, phase string) string {
	if phase == managedInstanceAlertEmailPhaseRecovery {
		return strconv.FormatInt(alertID, 10) + ":recovery"
	}
	return strconv.FormatInt(alertID, 10)
}

func enqueueDueManagedInstanceAlertEmails(now int64) {
	enqueueDueManagedInstanceAlertEmailPhase(now, managedInstanceAlertEmailPhaseFailure)
	enqueueDueManagedInstanceAlertEmailPhase(now, managedInstanceAlertEmailPhaseRecovery)
}

func enqueueDueManagedInstanceAlertEmailPhase(now int64, phase string) {
	var alerts []model.ManagedInstanceAlert
	query := model.DB.Order("first_seen_at asc, id asc").Limit(100)
	if phase == managedInstanceAlertEmailPhaseRecovery {
		query = query.Where(
			"status = ? AND recovery_email_status IN ? AND recovery_email_next_retry_at <= ?",
			model.ManagedInstanceAlertStatusResolved,
			[]string{model.ManagedInstanceAlertEmailPending, model.ManagedInstanceAlertEmailRetrying},
			now,
		)
	} else {
		query = query.Where(
			"status = ? AND email_status IN ? AND email_next_retry_at <= ?",
			model.ManagedInstanceAlertStatusOpen,
			[]string{"", model.ManagedInstanceAlertEmailPending, model.ManagedInstanceAlertEmailRetrying},
			now,
		)
	}
	if err := query.Find(&alerts).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("managed instance alert email query failed: phase=%s err=%v", phase, err))
		return
	}
	for _, alert := range alerts {
		if _, _, err := EnqueueScopedSystemTask(
			model.SystemTaskTypeManagedInstanceAlertEmail,
			managedInstanceAlertEmailScope(alert.Id, phase),
			managedInstanceAlertEmailPayload{AlertID: alert.Id, Phase: phase},
			nil,
		); err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed instance alert email enqueue failed: alert=%d phase=%s err=%v", alert.Id, phase, err))
		}
	}
}

// RetryManagedInstanceAlertEmailsNow applies SMTP fixes without waiting for a
// previous exponential-backoff deadline.
func RetryManagedInstanceAlertEmailsNow() error {
	now := common.GetTimestamp()
	if err := model.DB.Model(&model.ManagedInstanceAlert{}).
		Where("status = ? AND email_status IN ?", model.ManagedInstanceAlertStatusOpen, []string{model.ManagedInstanceAlertEmailPending, model.ManagedInstanceAlertEmailRetrying}).
		Update("email_next_retry_at", now).Error; err != nil {
		return err
	}
	if err := model.DB.Model(&model.ManagedInstanceAlert{}).
		Where("status = ? AND recovery_email_status IN ?", model.ManagedInstanceAlertStatusResolved, []string{model.ManagedInstanceAlertEmailPending, model.ManagedInstanceAlertEmailRetrying}).
		Update("recovery_email_next_retry_at", now).Error; err != nil {
		return err
	}
	enqueueDueManagedInstanceAlertEmails(now)
	notifySystemTaskRunner()
	return nil
}

func sendManagedInstanceAlertEmail(ctx context.Context, alertID int64) error {
	return sendManagedInstanceAlertEmailPhase(ctx, alertID, managedInstanceAlertEmailPhaseFailure)
}

func sendManagedInstanceAlertEmailPhase(ctx context.Context, alertID int64, phase string) error {
	var alert model.ManagedInstanceAlert
	if err := model.DB.First(&alert, alertID).Error; err != nil {
		return err
	}
	if phase == managedInstanceAlertEmailPhaseRecovery {
		return sendManagedInstanceRecoveryEmail(ctx, &alert)
	}
	if phase != managedInstanceAlertEmailPhaseFailure {
		return errors.New("invalid managed instance alert email phase")
	}
	if alert.EmailStatus == model.ManagedInstanceAlertEmailSent {
		return nil
	}
	if alert.EmailStatus == model.ManagedInstanceAlertEmailCancelled {
		return nil
	}
	if alert.Status != model.ManagedInstanceAlertStatusOpen {
		return updateManagedInstanceAlertProjection(alert.Id, map[string]any{
			"email_status": model.ManagedInstanceAlertEmailCancelled, "email_next_retry_at": 0,
		})
	}
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, alert.InstanceId).Error; err != nil {
		return recordManagedInstanceAlertEmailFailure(&alert, managedInstanceAlertEmailPhaseFailure, err)
	}
	recipients, err := billingalert.ParseRecipientList(alert.EmailRecipients)
	if err == nil && len(recipients) == 0 {
		err = billingalert.ErrSMTPNotConfigured
	}
	if err == nil {
		subject, textBody, htmlBody := managedInstanceAlertEmailContentPhase(&instance, &alert, false)
		err = billingalert.SendSMTPMessage(ctx, billingalert.SMTPMessage{
			Recipients: recipients, Subject: subject, TextBody: textBody, HTMLBody: htmlBody,
		})
	}
	if err != nil {
		return recordManagedInstanceAlertEmailFailure(&alert, managedInstanceAlertEmailPhaseFailure, err)
	}

	now := common.GetTimestamp()
	scheduleRecovery := false
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.ManagedInstanceAlert
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, alert.Id).Error; err != nil {
			return err
		}
		updates := map[string]any{
			"email_status": model.ManagedInstanceAlertEmailSent, "email_recipients": strings.Join(recipients, ","),
			"email_attempts": current.EmailAttempts + 1, "email_error": "", "email_sent_at": now,
			"email_next_retry_at": 0, "updated_at": now,
		}
		if current.Status == model.ManagedInstanceAlertStatusResolved && current.RecoveryEmailStatus != model.ManagedInstanceAlertEmailSent {
			updates["recovery_email_status"] = model.ManagedInstanceAlertEmailPending
			updates["recovery_email_error"] = ""
			updates["recovery_email_next_retry_at"] = now
			scheduleRecovery = true
		}
		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.First(&current, current.Id).Error; err != nil {
			return err
		}
		return managedinstance.SyncAlertEvents(tx, &instance, &current)
	})
	if err == nil && scheduleRecovery {
		notifySystemTaskRunner()
	}
	return err
}

func sendManagedInstanceRecoveryEmail(ctx context.Context, alert *model.ManagedInstanceAlert) error {
	if alert == nil {
		return errors.New("managed instance alert is required")
	}
	if alert.RecoveryEmailStatus == model.ManagedInstanceAlertEmailSent {
		return nil
	}
	if alert.RecoveryEmailStatus == model.ManagedInstanceAlertEmailCancelled {
		return nil
	}
	if alert.Status != model.ManagedInstanceAlertStatusResolved || alert.EmailStatus != model.ManagedInstanceAlertEmailSent {
		return updateManagedInstanceAlertProjection(alert.Id, map[string]any{
			"recovery_email_status":        model.ManagedInstanceAlertEmailCancelled,
			"recovery_email_next_retry_at": 0,
		})
	}
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, alert.InstanceId).Error; err != nil {
		return recordManagedInstanceAlertEmailFailure(alert, managedInstanceAlertEmailPhaseRecovery, err)
	}
	recipients, err := billingalert.ParseRecipientList(alert.EmailRecipients)
	if err == nil && len(recipients) == 0 {
		err = billingalert.ErrSMTPNotConfigured
	}
	if err == nil {
		subject, textBody, htmlBody := managedInstanceAlertEmailContentPhase(&instance, alert, true)
		err = billingalert.SendSMTPMessage(ctx, billingalert.SMTPMessage{
			Recipients: recipients, Subject: subject, TextBody: textBody, HTMLBody: htmlBody,
		})
	}
	if err != nil {
		return recordManagedInstanceAlertEmailFailure(alert, managedInstanceAlertEmailPhaseRecovery, err)
	}
	now := common.GetTimestamp()
	return updateManagedInstanceAlertProjection(alert.Id, map[string]any{
		"recovery_email_status":     model.ManagedInstanceAlertEmailSent,
		"recovery_email_recipients": strings.Join(recipients, ","),
		"recovery_email_attempts":   alert.RecoveryEmailAttempts + 1,
		"recovery_email_error":      "", "recovery_email_sent_at": now,
		"recovery_email_next_retry_at": 0, "updated_at": now,
	})
}

func recordManagedInstanceAlertEmailFailure(alert *model.ManagedInstanceAlert, phase string, deliveryErr error) error {
	if alert == nil {
		return deliveryErr
	}
	attempts := alert.EmailAttempts + 1
	statusField := "email_status"
	attemptsField := "email_attempts"
	errorField := "email_error"
	nextRetryField := "email_next_retry_at"
	if phase == managedInstanceAlertEmailPhaseRecovery {
		attempts = alert.RecoveryEmailAttempts + 1
		statusField = "recovery_email_status"
		attemptsField = "recovery_email_attempts"
		errorField = "recovery_email_error"
		nextRetryField = "recovery_email_next_retry_at"
	}
	delay := time.Minute * time.Duration(1<<min(attempts-1, 6))
	if delay > time.Hour {
		delay = time.Hour
	}
	now := common.GetTimestamp()
	if err := updateManagedInstanceAlertProjection(alert.Id, map[string]any{
		statusField: model.ManagedInstanceAlertEmailRetrying, attemptsField: attempts,
		errorField: deliveryErr.Error(), nextRetryField: now + int64(delay/time.Second), "updated_at": now,
	}); err != nil {
		return errors.Join(deliveryErr, err)
	}
	return deliveryErr
}

func updateManagedInstanceAlertProjection(alertID int64, updates map[string]any) error {
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var alert model.ManagedInstanceAlert
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&alert, alertID).Error; err != nil {
			return err
		}
		if err := tx.Model(&alert).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.First(&alert, alertID).Error; err != nil {
			return err
		}
		return managedinstance.SyncAlertEvents(tx, nil, &alert)
	})
}

func managedInstanceAlertEmailContent(instance *model.ManagedInstance, alert *model.ManagedInstanceAlert) (string, string, string) {
	return managedInstanceAlertEmailContentPhase(instance, alert, false)
}

func managedInstanceAlertEmailContentPhase(instance *model.ManagedInstance, alert *model.ManagedInstanceAlert, recovery bool) (string, string, string) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	eventAt := alert.LastSeenAt
	alertName := "实例不可用"
	if alert.AlertType == model.ManagedInstanceAlertTypeCredential {
		alertName = "实例凭据异常"
	}
	if recovery {
		alertName = "实例巡检已恢复"
		if alert.ResolvedAt > 0 {
			eventAt = alert.ResolvedAt
		}
	}
	eventTime := time.Unix(eventAt, 0).In(location).Format("2006-01-02 15:04:05")
	subject := fmt.Sprintf("[%s] %s", alertName, instance.Name)
	lines := [][2]string{
		{"实例", instance.Name},
		{"系统类型", instance.Kind},
		{"站点地址", instance.BaseURL},
		{"错误代码", alert.ErrorCode},
		{"连续失败", strconv.Itoa(instance.ConsecutiveFailures)},
	}
	if recovery {
		lines[len(lines)-1] = [2]string{"故障次数", strconv.Itoa(alert.Occurrences)}
	}
	lines = append(lines, [2]string{"巡检时间", eventTime + " (Asia/Shanghai)"})
	textLines := make([]string, 0, len(lines))
	var rows strings.Builder
	for _, line := range lines {
		textLines = append(textLines, line[0]+"："+line[1])
		rows.WriteString("<tr><th style=\"padding:6px 12px;text-align:left;color:#64748b\">")
		rows.WriteString(html.EscapeString(line[0]))
		rows.WriteString("</th><td style=\"padding:6px 12px\">")
		rows.WriteString(html.EscapeString(line[1]))
		rows.WriteString("</td></tr>")
	}
	return subject, strings.Join(textLines, "\n"), "<h2>" + html.EscapeString(alertName) + "</h2><table>" + rows.String() + "</table>"
}
