package model

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newExportScheduleTestPlan(t *testing.T) ManagedAccountExportSchedule {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&ManagedAccountExportSchedule{}, &ManagedAccountExportRun{}, &ManagedAccountExportDelivery{}))
	plan := ManagedAccountExportSchedule{OwnerID: 1, Name: t.Name(), Config: `{}`, Version: 1, Enabled: true, NextAt: 1000, CreatedAt: 1, UpdatedAt: 1}
	require.NoError(t, DB.Create(&plan).Error)
	t.Cleanup(func() {
		DB.Where("run_id IN (?)", DB.Model(&ManagedAccountExportRun{}).Select("id").Where("schedule_id = ?", plan.ID)).Delete(&ManagedAccountExportDelivery{})
		DB.Where("schedule_id = ?", plan.ID).Delete(&ManagedAccountExportRun{})
		DB.Delete(&ManagedAccountExportSchedule{}, plan.ID)
	})
	return plan
}

func TestAccountExportOccurrenceAtomicRollback(t *testing.T) {
	plan := newExportScheduleTestPlan(t)
	failure := errors.New("freeze failed")
	var taskID string
	err := DB.Transaction(func(tx *gorm.DB) error {
		_, err := ClaimAccountExportOccurrence(tx, plan, "scheduled:1000", 1000, 2000, 1000, false, "worker-a")
		require.NoError(t, err)
		task, err := CreateManagedUsageExportWithItemsTx(tx, &ManagedUsageExport{ActorID: 1, ActorName: "test", ExportKind: ManagedExportKindAccounts, Query: `{}`, FileFormat: ManagedExportFormatXLSX}, nil, nil, []*ManagedExportItem{{InstanceID: 1, ResourceID: 1, Metadata: `{}`}})
		require.NoError(t, err)
		taskID = task.TaskID
		return failure
	})
	require.ErrorIs(t, err, failure)
	var actual ManagedAccountExportSchedule
	require.NoError(t, DB.First(&actual, plan.ID).Error)
	require.Equal(t, plan.Version, actual.Version)
	require.Equal(t, int64(1000), actual.NextAt)
	require.Zero(t, actual.ActiveRunID)
	var count int64
	require.NoError(t, DB.Model(&ManagedAccountExportRun{}).Where("schedule_id = ?", plan.ID).Count(&count).Error)
	require.Zero(t, count)
	for _, entity := range []any{&SystemTask{}, &ManagedUsageExport{}, &ManagedExportItem{}} {
		require.NoError(t, DB.Model(entity).Where("task_id = ?", taskID).Count(&count).Error)
		require.Zero(t, count, "run, task, export and frozen items must roll back together")
	}
}

func TestAccountExportOccurrenceFencesWorkersAndSkipsOverlap(t *testing.T) {
	plan := newExportScheduleTestPlan(t)
	var first *ManagedAccountExportRun
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var err error
		first, err = ClaimAccountExportOccurrence(tx, plan, "scheduled:1000", 1000, 2000, 1000, false, "worker-a")
		return err
	}))
	require.ErrorIs(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := ClaimAccountExportOccurrence(tx, plan, "scheduled:1000", 1000, 2000, 1000, false, "worker-b")
		return err
	}), ErrExportScheduleConflict)
	require.NoError(t, DB.First(&plan, plan.ID).Error)
	require.Equal(t, first.ID, plan.ActiveRunID)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		run, err := ClaimAccountExportOccurrence(tx, plan, "scheduled:2000", 2000, 3000, 2000, false, "worker-b")
		if err == nil {
			require.Equal(t, "skipped", run.Status)
		}
		return err
	}))
	require.NoError(t, DB.First(&plan, plan.ID).Error)
	require.Equal(t, first.ID, plan.ActiveRunID)
	require.Equal(t, int64(3000), plan.NextAt)
}

func TestManualAccountExportPreservesNextTimeAndPauseFences(t *testing.T) {
	plan := newExportScheduleTestPlan(t)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := ClaimAccountExportOccurrence(tx, plan, "manual:unique", 900, 0, 900, true, "worker")
		return err
	}))
	require.NoError(t, DB.First(&plan, plan.ID).Error)
	require.Equal(t, int64(1000), plan.NextAt)
	require.NoError(t, DB.Model(&plan).Update("enabled", false).Error)
	require.ErrorIs(t, DB.Transaction(func(tx *gorm.DB) error {
		_, err := ClaimAccountExportOccurrence(tx, plan, "scheduled:1000", 1000, 2000, 1000, false, "worker")
		return err
	}), ErrExportScheduleConflict)
}

func TestInterruptedAccountExportDeliveryRequiresReview(t *testing.T) {
	plan := newExportScheduleTestPlan(t)
	run := ManagedAccountExportRun{ScheduleID: plan.ID, Occurrence: "test", ScheduledAt: 1000, ScheduleVersion: 1, FrozenConfig: `{}`, Status: "delivering", CreatedAt: 1000}
	require.NoError(t, DB.Create(&run).Error)
	delivery := ManagedAccountExportDelivery{RunID: run.ID, Recipient: "test@example.com", Status: "sending", Version: 1, LeaseOwner: "lost", LeaseUntil: 1050, CreatedAt: 1000}
	require.NoError(t, DB.Create(&delivery).Error)
	require.NoError(t, RecoverAccountExportDeliveries(DB, 1100))
	require.NoError(t, DB.First(&delivery, delivery.ID).Error)
	require.Equal(t, "uncertain", delivery.Status)
	require.Empty(t, delivery.LeaseOwner)
	require.Equal(t, int64(2), delivery.Version)
	require.NoError(t, RecoverAccountExportDeliveries(DB, 1200))
	require.NoError(t, DB.First(&delivery, delivery.ID).Error)
	require.Equal(t, int64(2), delivery.Version)
}
