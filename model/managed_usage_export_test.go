package model

import (
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/stretchr/testify/require"
)

func TestManagedUsageExportsQueueIndependentlyAndCancelPending(t *testing.T) {
	truncateTables(t)
	firstRecord := &ManagedUsageExport{InstanceID: 1, InstanceName: "one", InstanceKind: ManagedInstanceKindSub2API, ActorID: 10, ActorName: "admin", Query: `{}`}
	first, err := CreateManagedUsageExport(firstRecord, map[string]int{"instance_id": 1}, map[string]string{"stage": "queued"})
	require.NoError(t, err)
	secondRecord := &ManagedUsageExport{InstanceID: 2, InstanceName: "two", InstanceKind: ManagedInstanceKindNewAPI, ActorID: 11, ActorName: "other", Query: `{}`}
	second, err := CreateManagedUsageExport(secondRecord, map[string]int{"instance_id": 2}, map[string]string{"stage": "queued"})
	require.NoError(t, err)
	require.NotEqual(t, first.TaskID, second.TaskID)
	require.Empty(t, first.ScopeKey)
	require.Empty(t, second.ScopeKey)
	require.Equal(t, int64(1), ManagedUsageExportQueuePosition(firstRecord))
	require.Equal(t, int64(2), ManagedUsageExportQueuePosition(secondRecord))
	list, err := ListManagedUsageExports(ManagedUsageExportListFilter{Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(2), list.Total)
	require.True(t, list.HasActive)
	require.Len(t, list.Items, 1)
	require.Equal(t, first.TaskID, list.Items[0].TaskID)

	require.ErrorIs(t, CancelManagedUsageExport(second.TaskID, 99, false), ErrManagedUsageExportConflict)
	require.NoError(t, CancelManagedUsageExport(second.TaskID, 10, true))
	reloaded, err := GetManagedUsageExport(second.TaskID)
	require.NoError(t, err)
	require.Equal(t, ManagedUsageExportStatusCancelled, reloaded.Status)
	task, err := GetSystemTaskByTaskID(second.TaskID)
	require.NoError(t, err)
	require.Equal(t, SystemTaskStatusCancelled, task.Status)
}

func TestManagedUsageExportsUseOneGlobalFIFOLease(t *testing.T) {
	truncateTables(t)
	first, err := CreateManagedUsageExport(
		&ManagedUsageExport{InstanceID: 1, InstanceName: "one", InstanceKind: ManagedInstanceKindSub2API, ActorID: 10, ActorName: "admin", Query: `{}`},
		map[string]int{"instance_id": 1}, nil,
	)
	require.NoError(t, err)
	second, err := CreateManagedUsageExport(
		&ManagedUsageExport{InstanceID: 2, InstanceName: "two", InstanceKind: ManagedInstanceKindNewAPI, ActorID: 11, ActorName: "other", Query: `{}`},
		map[string]int{"instance_id": 2}, nil,
	)
	require.NoError(t, err)

	pending, err := FindPendingSystemTasks(SystemTaskTypeManagedUsageExport, 10)
	require.NoError(t, err)
	require.Len(t, pending, 2)
	require.Equal(t, []string{first.TaskID, second.TaskID}, []string{pending[0].TaskID, pending[1].TaskID})

	claimedFirst, ok, err := ClaimSystemTask(first.ID, first.Type, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	_, ok, err = ClaimSystemTask(second.ID, second.Type, "runner-b", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, FinishSystemTask(claimedFirst.TaskID, "runner-a", SystemTaskStatusSucceeded, nil, ""))
	_, ok, err = ClaimSystemTask(second.ID, second.Type, "runner-b", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestManagedUsageExportLeaseExpiryReturnsTaskToQueue(t *testing.T) {
	truncateTables(t)
	record := &ManagedUsageExport{InstanceID: 1, InstanceName: "one", InstanceKind: ManagedInstanceKindSub2API, ActorID: 10, ActorName: "admin", Query: `{}`}
	task, err := CreateManagedUsageExport(record, map[string]int{"instance_id": 1}, nil)
	require.NoError(t, err)
	claimed, ok, err := ClaimSystemTask(task.ID, task.Type, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, StartManagedUsageExport(claimed.TaskID))
	require.NoError(t, MarkSystemTaskLeaseExpired(claimed.TaskID))

	reloadedTask, err := GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, SystemTaskStatusPending, reloadedTask.Status)
	reloaded, err := GetManagedUsageExport(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, ManagedUsageExportStatusPending, reloaded.Status)
	require.Zero(t, reloaded.StartedAt)
}

func TestManagedUsageExportGracefulInterruptionReturnsTaskToQueue(t *testing.T) {
	truncateTables(t)
	record := &ManagedUsageExport{InstanceID: 1, InstanceName: "one", InstanceKind: ManagedInstanceKindSub2API, ActorID: 10, ActorName: "admin", Query: `{}`}
	task, err := CreateManagedUsageExport(record, map[string]int{"instance_id": 1}, nil)
	require.NoError(t, err)
	claimed, ok, err := ClaimSystemTask(task.ID, task.Type, "runner-a", common.GetTimestamp()+60)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, StartManagedUsageExport(claimed.TaskID))
	require.NoError(t, RequeueManagedUsageExport(claimed.TaskID, "runner-a"))

	reloadedTask, err := GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, SystemTaskStatusPending, reloadedTask.Status)
	reloaded, err := GetManagedUsageExport(task.TaskID)
	require.NoError(t, err)
	require.Equal(t, ManagedUsageExportStatusPending, reloaded.Status)
}

func TestExpireManagedUsageExportsKeepsRecord(t *testing.T) {
	truncateTables(t)
	record := &ManagedUsageExport{
		TaskID: "systask_expired", InstanceID: 1, InstanceName: "one",
		InstanceKind: ManagedInstanceKindSub2API, ActorID: 10, ActorName: "admin",
		Query: `{}`, Status: ManagedUsageExportStatusSucceeded, ExpiresAt: 100,
	}
	require.NoError(t, DB.Create(record).Error)
	records, err := ExpireManagedUsageExports(101)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "systask_expired", records[0].TaskID)
	reloaded, err := GetManagedUsageExport("systask_expired")
	require.NoError(t, err)
	require.Equal(t, ManagedUsageExportStatusExpired, reloaded.Status)
}

func TestManagedUsageExportListIncludesCompletedRecords(t *testing.T) {
	truncateTables(t)
	record := &ManagedUsageExport{
		TaskID: "systask_completed", InstanceID: 1, InstanceName: "one",
		InstanceKind: ManagedInstanceKindSub2API, ActorID: 10, ActorName: "admin",
		Query: `{}`, Status: ManagedUsageExportStatusSucceeded, ExpiresAt: common.GetTimestamp() + 3600,
	}
	require.NoError(t, DB.Create(record).Error)

	list, err := ListManagedUsageExports(ManagedUsageExportListFilter{ActorID: 10})
	require.NoError(t, err)
	require.False(t, list.HasActive)
	require.Equal(t, int64(1), list.Total)
	require.Len(t, list.Items, 1)
	require.Equal(t, record.TaskID, list.Items[0].TaskID)
}

func TestCreateManagedAccountExportPersistsFrozenItems(t *testing.T) {
	truncateTables(t)
	record := &ManagedUsageExport{
		InstanceName: "two instances", InstanceKind: "mixed", ActorID: 10, ActorName: "admin",
		ExportKind: ManagedExportKindAccounts, FileFormat: ManagedExportFormatXLSX, Query: `{}`,
	}
	task, err := CreateManagedUsageExportWithItems(record, map[string]any{"export_kind": ManagedExportKindAccounts}, map[string]any{}, []*ManagedExportItem{
		{InstanceID: 1, ResourceID: 42, Metadata: `{"account":{"id":42}}`},
		{InstanceID: 2, ResourceID: 42, Metadata: `{"account":{"id":42}}`},
	})
	require.NoError(t, err)
	items, err := ListManagedExportItems(task.TaskID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, int64(1), items[0].InstanceID)
	require.Equal(t, int64(2), items[1].InstanceID)
	require.Equal(t, ManagedExportKindAccounts, record.ExportKind)
	require.Equal(t, ManagedExportFormatXLSX, record.FileFormat)
}

func TestDeleteManagedUsageExportOnlyDeletesOwnedTerminalRecord(t *testing.T) {
	truncateTables(t)
	pendingRecord := &ManagedUsageExport{InstanceID: 1, InstanceName: "one", InstanceKind: ManagedInstanceKindSub2API, ActorID: 10, ActorName: "admin", Query: `{}`}
	pendingTask, err := CreateManagedUsageExport(pendingRecord, map[string]int{"instance_id": 1}, nil)
	require.NoError(t, err)
	_, err = DeleteManagedUsageExport(pendingTask.TaskID, 10, false)
	require.ErrorIs(t, err, ErrManagedUsageExportConflict)

	finishedRecord := &ManagedUsageExport{InstanceID: 1, InstanceName: "one", InstanceKind: ManagedInstanceKindSub2API, ActorID: 10, ActorName: "admin", Query: `{}`}
	finishedTask, err := CreateManagedUsageExport(finishedRecord, map[string]int{"instance_id": 1}, nil)
	require.NoError(t, err)
	require.NoError(t, DB.Model(&ManagedUsageExport{}).Where("task_id = ?", finishedTask.TaskID).Update("status", ManagedUsageExportStatusFailed).Error)
	require.NoError(t, DB.Model(&SystemTask{}).Where("task_id = ?", finishedTask.TaskID).Updates(map[string]any{"status": SystemTaskStatusFailed, "active_key": nil}).Error)

	deleted, err := DeleteManagedUsageExport(finishedTask.TaskID, 11, false)
	require.NoError(t, err)
	require.Nil(t, deleted)
	deleted, err = DeleteManagedUsageExport(finishedTask.TaskID, 10, false)
	require.NoError(t, err)
	require.Equal(t, finishedTask.TaskID, deleted.TaskID)
	record, err := GetManagedUsageExport(finishedTask.TaskID)
	require.NoError(t, err)
	require.Nil(t, record)
	task, err := GetSystemTaskByTaskID(finishedTask.TaskID)
	require.NoError(t, err)
	require.Nil(t, task)
}
