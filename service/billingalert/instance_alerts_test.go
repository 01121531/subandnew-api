package billingalert

import (
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestListInstanceAlertsFiltersAndRecordDelivery(t *testing.T) {
	setupRepositoryTestDB(t)
	instance := &model.ManagedInstance{Name: "gateway-east", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway.example.com"}
	require.NoError(t, model.DB.Create(instance).Error)
	alert := &model.ManagedInstanceAlert{
		InstanceId: instance.Id, AlertType: model.ManagedInstanceAlertTypeCredential,
		Status: model.ManagedInstanceAlertStatusResolved, ErrorCode: "auth_failed",
		Occurrences: 2, FirstSeenAt: 100, LastSeenAt: 150, ResolvedAt: 180,
		EmailStatus: model.ManagedInstanceAlertEmailSent, EmailRecipients: "ops@example.com",
		EmailAttempts: 1, EmailSentAt: 120,
		RecoveryEmailStatus:     model.ManagedInstanceAlertEmailRetrying,
		RecoveryEmailRecipients: "ops@example.com", RecoveryEmailAttempts: 2,
		RecoveryEmailError: "smtp_timeout", RecoveryEmailNextRetryAt: 240,
	}
	require.NoError(t, model.DB.Create(alert).Error)

	page, err := ListInstanceAlerts(InstanceAlertFilter{
		Status: model.ManagedInstanceAlertStatusResolved, AlertType: model.ManagedInstanceAlertTypeCredential,
		DeliveryStatus: model.ManagedInstanceAlertEmailRetrying, Search: "gateway-east",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Equal(t, "gateway-east", page.Items[0].InstanceName)
	require.Equal(t, model.ManagedInstanceKindClaudeGateway, page.Items[0].InstanceKind)

	event := &model.BillingAlertEvent{
		EventKey: "instance-alert:test:recovery", EventType: model.InstanceAlertEventRecovered,
		SourceType: model.AlertSourceInstance, SourceRecordID: alert.Id, InstanceID: instance.Id,
		RuleName: "实例巡检", InstanceName: instance.Name, InstanceKind: instance.Kind,
		Recipients: alert.RecoveryEmailRecipients, ErrorCode: alert.ErrorCode, CreatedAt: alert.ResolvedAt,
	}
	require.NoError(t, model.DB.Create(event).Error)
	record, err := GetAlertRecord(event.ID)
	require.NoError(t, err)
	require.Len(t, record.Deliveries, 1)
	require.Equal(t, "recovery", record.Deliveries[0].Phase)
	require.Equal(t, model.ManagedInstanceAlertEmailRetrying, record.Deliveries[0].Status)
	require.Equal(t, "smtp_timeout", record.Deliveries[0].LastError)
}
