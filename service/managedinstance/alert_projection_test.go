package managedinstance

import (
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestRepairAlertEventProjectionsBackfillsIdempotently(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instance := &model.ManagedInstance{Name: "legacy-conductor", Kind: model.ManagedInstanceKindConductor, BaseURL: "https://legacy.example.com"}
	require.NoError(t, db.Create(instance).Error)
	alert := &model.ManagedInstanceAlert{
		InstanceId: instance.Id, AlertType: model.ManagedInstanceAlertTypeCredential,
		Status: model.ManagedInstanceAlertStatusResolved, ErrorCode: "permission_denied",
		Occurrences: 4, FirstSeenAt: 100, LastSeenAt: 180, ResolvedAt: 200,
		EmailStatus: model.ManagedInstanceAlertEmailSent, EmailRecipients: "ops@example.com",
		RecoveryEmailStatus: model.ManagedInstanceAlertEmailSent, RecoveryEmailRecipients: "ops@example.com",
	}
	require.NoError(t, db.Create(alert).Error)

	first, err := RepairAlertEventProjections()
	require.NoError(t, err)
	require.Equal(t, 1, first.Processed)
	second, err := RepairAlertEventProjections()
	require.NoError(t, err)
	require.Equal(t, 1, second.Processed)

	var events []model.BillingAlertEvent
	require.NoError(t, db.Order("created_at ASC").Find(&events).Error)
	require.Len(t, events, 2)
	require.Equal(t, model.InstanceAlertEventFailure, events[0].EventType)
	require.Equal(t, int64(100), events[0].CreatedAt)
	require.Equal(t, model.InstanceAlertEventRecovered, events[1].EventType)
	require.Equal(t, int64(200), events[1].CreatedAt)
	for _, event := range events {
		require.Equal(t, model.AlertSourceInstance, event.SourceType)
		require.Equal(t, alert.Id, event.SourceRecordID)
		require.Equal(t, "legacy-conductor", event.InstanceName)
	}
}
