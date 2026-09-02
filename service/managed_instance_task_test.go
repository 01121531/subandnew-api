package service

import (
	"context"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestBoundedManagedInstanceOperationConcurrency(t *testing.T) {
	require.Equal(t, 1, boundedManagedInstanceOperationConcurrency(0))
	require.Equal(t, 4, boundedManagedInstanceOperationConcurrency(4))
	require.Equal(t, 32, boundedManagedInstanceOperationConcurrency(100))
}

func TestManagedInstanceOperationScopedSlotLifecycle(t *testing.T) {
	store := map[string]*managedInstanceOperationScopedSlot{}
	first := retainManagedInstanceOperationScopedSlot(store, "batch-a", 1)
	require.NoError(t, acquireManagedInstanceOperationSlot(context.Background(), first.slots))
	second := retainManagedInstanceOperationScopedSlot(store, "batch-a", 1)
	require.Same(t, first, second)

	releaseManagedInstanceOperationScopedSlot(store, "batch-a", first, true)
	require.Same(t, second, store["batch-a"])
	require.NoError(t, acquireManagedInstanceOperationSlot(context.Background(), second.slots))
	releaseManagedInstanceOperationScopedSlot(store, "batch-a", second, true)
	require.NotContains(t, store, "batch-a")
}

func TestBatchCoordinatorPersistsNeedsReconciliationWithoutClientPoll(t *testing.T) {
	truncate(t)
	now := time.Now().Unix()
	batch := &model.ManagedInstanceOperationBatch{
		BatchId: "mibatch_background_reconcile", ActorId: 1, ExecutedBy: 2,
		Action: model.ManagedInstanceActionToggleResource, Status: model.ManagedInstanceBatchStatusQueued,
		TargetCount: 1, IdempotencyKey: "parent-digest", IdempotencyFingerprint: "fingerprint",
		PlanHash: "plan-digest", PlannedAt: now, ExecutedAt: now,
	}
	require.NoError(t, model.DB.Create(batch).Error)
	operation := &model.ManagedInstanceOperation{
		OperationId: "miop_background_unknown", InstanceId: 1001, ActorId: 1, ExecutedBy: 2,
		Action: model.ManagedInstanceActionToggleResource, Status: model.ManagedInstanceOperationStatusUnknown,
		RiskLevel: "low", WritesRemote: true, RequiredCapability: "channels.toggle",
		IdempotencyKey: "child-digest", IdempotencyFingerprint: "fingerprint",
		Parameters: `{}`, Plan: `{}`, ErrorCode: "remote_result_unknown",
		PlannedAt: now, ExecutedAt: now, FinishedAt: now,
	}
	require.NoError(t, model.DB.Create(operation).Error)
	require.NoError(t, model.DB.Create(&model.ManagedInstanceOperationBatchItem{
		BatchId: batch.BatchId, InstanceId: operation.InstanceId, OperationId: operation.OperationId,
		Position: 0, Status: model.ManagedInstanceOperationStatusQueued, Parameters: `{}`,
	}).Error)

	resumeManagedInstanceOperationBatches()
	var reloaded model.ManagedInstanceOperationBatch
	require.NoError(t, model.DB.Where("batch_id = ?", batch.BatchId).First(&reloaded).Error)
	require.Equal(t, model.ManagedInstanceBatchStatusNeedsReconcile, reloaded.Status)
	require.NotZero(t, reloaded.FinishedAt)
}

func TestManagedUsageExportViewDropsNullLegacyFilters(t *testing.T) {
	record := &model.ManagedUsageExport{
		ExportKind: model.ManagedExportKindUsageRecords,
		Query:      `{"username":null,"model":["claude-sonnet"]}`,
	}

	view := managedUsageExportView(record)

	require.NotNil(t, view)
	require.NotContains(t, view.Filters, "username")
	require.Equal(t, []string{"claude-sonnet"}, view.Filters["model"])
}
