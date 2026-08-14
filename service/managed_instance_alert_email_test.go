package service

import (
	"context"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/billingalert"
	"github.com/stretchr/testify/require"
)

func TestManagedInstanceAlertEmailSchedulerDeduplicatesAndRetries(t *testing.T) {
	truncate(t)
	instance := &model.ManagedInstance{Name: "conductor-a", Kind: model.ManagedInstanceKindConductor, BaseURL: "https://example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	alert := &model.ManagedInstanceAlert{
		InstanceId: instance.Id, AlertType: model.ManagedInstanceAlertTypeAvailability,
		Status: model.ManagedInstanceAlertStatusOpen, ErrorCode: "remote_http_error",
	}
	require.NoError(t, model.DB.Create(alert).Error)

	now := common.GetTimestamp()
	enqueueDueManagedInstanceAlertEmails(now)
	enqueueDueManagedInstanceAlertEmails(now)
	var tasks []model.SystemTask
	require.NoError(t, model.DB.Where("type = ?", model.SystemTaskTypeManagedInstanceAlertEmail).Find(&tasks).Error)
	require.Len(t, tasks, 1)

	err := sendManagedInstanceAlertEmail(context.Background(), alert.Id)
	require.ErrorIs(t, err, billingalert.ErrSMTPNotConfigured)
	require.NoError(t, model.DB.First(alert, alert.Id).Error)
	require.Equal(t, model.ManagedInstanceAlertEmailRetrying, alert.EmailStatus)
	require.Equal(t, 1, alert.EmailAttempts)
	require.Greater(t, alert.EmailNextRetryAt, now)
}

func TestManagedInstanceAlertEmailContentIncludesInstanceAndFailure(t *testing.T) {
	instance := &model.ManagedInstance{Name: "prod-a", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://api.example.com", ConsecutiveFailures: 3}
	alert := &model.ManagedInstanceAlert{AlertType: model.ManagedInstanceAlertTypeAvailability, ErrorCode: "connector_failed", LastSeenAt: 1_786_600_000}
	subject, textBody, htmlBody := managedInstanceAlertEmailContent(instance, alert)
	require.Contains(t, subject, "prod-a")
	require.Contains(t, textBody, "connector_failed")
	require.Contains(t, textBody, "连续失败：3")
	require.Contains(t, htmlBody, "api.example.com")
}
