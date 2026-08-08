package managedinstance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestPlanBatchOperationIsStableIdempotentAndReportsPartialPlans(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	first := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	second := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	unsupported := createOperationTestInstance(t, model.ManagedInstanceModeObserve)
	key := "batch-plan-idempotency-key"

	planned, err := PlanBatchOperation(PlanBatchOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: key, ActorID: 51,
		Targets: []BatchOperationTargetInput{
			{InstanceID: unsupported.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: second.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
		},
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceBatchStatusPartiallyPlanned, planned.Status)
	require.Equal(t, BatchOperationSummary{Total: 3, Planned: 2, Failed: 1}, planned.Summary)
	require.Len(t, planned.Items, 3)
	require.Equal(t, first.Id, planned.Items[0].InstanceID)
	require.Equal(t, second.Id, planned.Items[1].InstanceID)
	require.Equal(t, unsupported.Id, planned.Items[2].InstanceID)
	require.Equal(t, "unsupported_capability", planned.Items[2].ErrorCode)
	require.Nil(t, planned.Items[2].Operation)

	replayed, err := PlanBatchOperation(PlanBatchOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: key, ActorID: 51,
		Targets: []BatchOperationTargetInput{
			{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: unsupported.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: second.Id, Parameters: json.RawMessage(`{}`)},
		},
	})
	require.NoError(t, err)
	require.True(t, replayed.IdempotentReplay)
	require.Equal(t, planned.BatchId, replayed.BatchId)

	_, err = PlanBatchOperation(PlanBatchOperationInput{
		Action: model.ManagedInstanceActionTestResources, IdempotencyKey: key, ActorID: 51,
		Targets: []BatchOperationTargetInput{
			{InstanceID: first.Id, Parameters: json.RawMessage(`{"resource_ids":[1]}`)},
			{InstanceID: second.Id, Parameters: json.RawMessage(`{"resource_ids":[1]}`)},
		},
	})
	require.ErrorIs(t, err, ErrIdempotencyConflict)

	_, err = PlanBatchOperation(PlanBatchOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: "duplicate-target-key", ActorID: 51,
		Targets: []BatchOperationTargetInput{
			{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
		},
	})
	require.ErrorIs(t, err, ErrInvalidOperation)

	var batches int64
	require.NoError(t, db.Model(&model.ManagedInstanceOperationBatch{}).Count(&batches).Error)
	require.Equal(t, int64(1), batches)
	var stored model.ManagedInstanceOperationBatch
	require.NoError(t, db.First(&stored).Error)
	require.NotEqual(t, key, stored.IdempotencyKey)
	require.Len(t, stored.IdempotencyKey, 64)
	encoded, err := json.Marshal(planned)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), key)

	var audits []model.ManagedInstanceAudit
	require.NoError(t, db.Where("action = ?", "batch_operation_plan").Find(&audits).Error)
	require.Len(t, audits, 3)
	for _, audit := range audits {
		require.Contains(t, audit.Details, planned.BatchId)
		require.NotContains(t, audit.Details, key)
	}
}

func TestPlanBatchOperationFinishesWhenNoTargetCanBePlanned(t *testing.T) {
	setupManagedInstanceOperationTestDB(t)
	unsupported := createOperationTestInstance(t, model.ManagedInstanceModeObserve)
	view, err := PlanBatchOperation(PlanBatchOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: "batch-all-plan-failures", ActorID: 52,
		Targets: []BatchOperationTargetInput{
			{InstanceID: unsupported.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: unsupported.Id + 10_000, Parameters: json.RawMessage(`{}`)},
		},
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceBatchStatusFailed, view.Status)
	require.Equal(t, BatchOperationSummary{Total: 2, Failed: 2}, view.Summary)
	require.NotZero(t, view.FinishedAt)
}

func TestBatchPlanReservationRejectsDifferentConcurrentPlanBeforeCreatingChildren(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	first := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	second := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	third := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	key := "batch-reservation-conflict"
	input := PlanBatchOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: key, ActorID: 53,
	}
	reservedTargets, err := normalizeBatchTargets(input.Action, []BatchOperationTargetInput{
		{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
		{InstanceID: second.Id, Parameters: json.RawMessage(`{}`)},
	})
	require.NoError(t, err)
	_, _, err = reserveBatchOperation(input, batchOperationPlanHash(input.Action, reservedTargets), len(reservedTargets))
	require.NoError(t, err)

	_, err = PlanBatchOperation(PlanBatchOperationInput{
		Action: input.Action, IdempotencyKey: key, ActorID: input.ActorID,
		Targets: []BatchOperationTargetInput{
			{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: third.Id, Parameters: json.RawMessage(`{}`)},
		},
	})
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	var operationCount int64
	require.NoError(t, db.Model(&model.ManagedInstanceOperation{}).Count(&operationCount).Error)
	require.Zero(t, operationCount)
}

func TestExecuteBatchOperationQueuesChildrenAndConverges(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	first := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	second := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	key := "batch-execute-idempotency-key"
	planned, err := PlanBatchOperation(PlanBatchOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: key, ActorID: 61,
		Targets: []BatchOperationTargetInput{
			{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: second.Id, Parameters: json.RawMessage(`{}`)},
		},
	})
	require.NoError(t, err)

	queued, err := ExecuteBatchOperation(ExecuteBatchOperationInput{
		BatchID: planned.BatchId, IdempotencyKey: key, ActorID: 62,
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceBatchStatusQueued, queued.Status)
	require.Equal(t, 2, queued.Summary.Active)
	for _, item := range queued.Items {
		require.NotNil(t, item.Operation)
		require.Equal(t, model.ManagedInstanceOperationStatusQueued, item.Operation.Status)
	}

	replayed, err := ExecuteBatchOperation(ExecuteBatchOperationInput{
		BatchID: planned.BatchId, IdempotencyKey: key, ActorID: 62,
	})
	require.NoError(t, err)
	require.Equal(t, planned.BatchId, replayed.BatchId)
	var taskCount int64
	require.NoError(t, db.Model(&model.SystemTask{}).
		Where("type = ?", model.SystemTaskTypeManagedInstanceOperation).Count(&taskCount).Error)
	require.Equal(t, int64(2), taskCount)
	var tasks []model.SystemTask
	require.NoError(t, db.Where("type = ?", model.SystemTaskTypeManagedInstanceOperation).Find(&tasks).Error)
	for _, task := range tasks {
		var payload struct {
			BatchID string `json:"batch_id"`
		}
		require.NoError(t, json.Unmarshal([]byte(task.Payload), &payload))
		require.Equal(t, planned.BatchId, payload.BatchID)
	}

	previousExecutor := executeManagedInstanceRemoteOperation
	executeManagedInstanceRemoteOperation = func(_ context.Context, _ *model.ManagedInstance, _ *CredentialMaterial, action string, _ json.RawMessage) (*remoteOperationResult, error) {
		return &remoteOperationResult{Action: action, ResourceKind: "channel", Count: 1}, nil
	}
	t.Cleanup(func() { executeManagedInstanceRemoteOperation = previousExecutor })
	for _, item := range queued.Items {
		_, err := RunOperation(context.Background(), item.Operation.OperationId, item.Operation.TaskId)
		require.NoError(t, err)
	}

	completed, err := GetBatchOperation(planned.BatchId)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceBatchStatusSucceeded, completed.Status)
	require.Equal(t, BatchOperationSummary{Total: 2, Succeeded: 2}, completed.Summary)
	require.NotZero(t, completed.FinishedAt)
}

func TestResumeBatchOperationQueuesChildrenAfterInterruptedExecute(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	first := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	second := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	planned, err := PlanBatchOperation(PlanBatchOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: "batch-recovery-key", ActorID: 63,
		Targets: []BatchOperationTargetInput{
			{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: second.Id, Parameters: json.RawMessage(`{}`)},
		},
	})
	require.NoError(t, err)
	now := time.Now().Unix()
	require.NoError(t, db.Model(&model.ManagedInstanceOperationBatch{}).
		Where("batch_id = ?", planned.BatchId).
		Updates(map[string]any{
			"status": model.ManagedInstanceBatchStatusQueued, "executed_by": 64,
			"executed_at": now, "updated_at": now,
		}).Error)

	resumed, err := ResumeBatchOperation(planned.BatchId)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceBatchStatusQueued, resumed.Status)
	require.Equal(t, 2, resumed.Summary.Active)
	var taskCount int64
	require.NoError(t, db.Model(&model.SystemTask{}).
		Where("type = ?", model.SystemTaskTypeManagedInstanceOperation).Count(&taskCount).Error)
	require.Equal(t, int64(2), taskCount)
	for _, item := range resumed.Items {
		require.NotNil(t, item.Operation)
		require.Equal(t, model.ManagedInstanceOperationStatusQueued, item.Operation.Status)
	}
}

func TestBatchExecuteReplayUsesRecordedExecutor(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	first := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	second := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	key := "batch-recorded-executor-key"
	planned, err := PlanBatchOperation(PlanBatchOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: key, ActorID: 65,
		Targets: []BatchOperationTargetInput{
			{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: second.Id, Parameters: json.RawMessage(`{}`)},
		},
	})
	require.NoError(t, err)
	_, err = ExecuteBatchOperation(ExecuteBatchOperationInput{
		BatchID: planned.BatchId, IdempotencyKey: key, ActorID: 66,
	})
	require.NoError(t, err)
	_, err = ExecuteBatchOperation(ExecuteBatchOperationInput{
		BatchID: planned.BatchId, IdempotencyKey: key, ActorID: 67,
	})
	require.NoError(t, err)

	var batch model.ManagedInstanceOperationBatch
	require.NoError(t, db.Where("batch_id = ?", planned.BatchId).First(&batch).Error)
	require.Equal(t, 66, batch.ExecutedBy)
	var operations []model.ManagedInstanceOperation
	require.NoError(t, db.Find(&operations).Error)
	require.Len(t, operations, 2)
	for _, operation := range operations {
		require.Equal(t, 66, operation.ExecutedBy)
	}
	var replayAudits int64
	require.NoError(t, db.Model(&model.ManagedInstanceAudit{}).
		Where("action = ? AND actor_id = ?", "batch_operation_execute", 67).Count(&replayAudits).Error)
	require.Zero(t, replayAudits)
	var executeAudits int64
	require.NoError(t, db.Model(&model.ManagedInstanceAudit{}).
		Where("action = ?", "batch_operation_execute").Count(&executeAudits).Error)
	require.Equal(t, int64(2), executeAudits)
}

func TestExecuteBatchOperationPreservesPartialEnqueueFailure(t *testing.T) {
	setupManagedInstanceOperationTestDB(t)
	first := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	second := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	key := "batch-partial-execute-key"
	planned, err := PlanBatchOperation(PlanBatchOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: key, ActorID: 71,
		Targets: []BatchOperationTargetInput{
			{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: second.Id, Parameters: json.RawMessage(`{}`)},
		},
	})
	require.NoError(t, err)

	blockingPlan, err := PlanOperation(second.Id, PlanOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: "blocking-operation-key",
		Parameters: json.RawMessage(`{}`), ActorID: 71,
	})
	require.NoError(t, err)
	_, _, err = ExecuteOperation(second.Id, ExecuteOperationInput{
		OperationID: blockingPlan.OperationId, IdempotencyKey: "blocking-operation-key", ActorID: 71,
	})
	require.NoError(t, err)

	queued, err := ExecuteBatchOperation(ExecuteBatchOperationInput{
		BatchID: planned.BatchId, IdempotencyKey: key, ActorID: 72,
	})
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceBatchStatusQueued, queued.Status)
	require.Equal(t, 1, queued.Summary.Active)
	require.Equal(t, 1, queued.Summary.Failed)
	var failedItem *BatchOperationItemView
	for index := range queued.Items {
		if queued.Items[index].InstanceID == second.Id {
			failedItem = &queued.Items[index]
		}
	}
	require.NotNil(t, failedItem)
	require.Equal(t, "operation_busy", failedItem.ErrorCode)

	active := queued.Items[0]
	if active.InstanceID == second.Id {
		active = queued.Items[1]
	}
	previousExecutor := executeManagedInstanceRemoteOperation
	executeManagedInstanceRemoteOperation = func(_ context.Context, _ *model.ManagedInstance, _ *CredentialMaterial, action string, _ json.RawMessage) (*remoteOperationResult, error) {
		return &remoteOperationResult{Action: action, ResourceKind: "channel", Count: 1}, nil
	}
	t.Cleanup(func() { executeManagedInstanceRemoteOperation = previousExecutor })
	_, err = RunOperation(context.Background(), active.Operation.OperationId, active.Operation.TaskId)
	require.NoError(t, err)

	completed, err := GetBatchOperation(planned.BatchId)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceBatchStatusPartiallyFailed, completed.Status)
	require.Equal(t, BatchOperationSummary{Total: 2, Succeeded: 1, Failed: 1}, completed.Summary)
}

func TestBatchOperationRequiresReconciliationForUnknownChild(t *testing.T) {
	db := setupManagedInstanceOperationTestDB(t)
	first := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	second := createOperationTestInstance(t, model.ManagedInstanceModeObserve, "channels.list")
	key := "batch-unknown-result-key"
	planned, err := PlanBatchOperation(PlanBatchOperationInput{
		Action: model.ManagedInstanceActionRefreshInventory, IdempotencyKey: key, ActorID: 81,
		Targets: []BatchOperationTargetInput{
			{InstanceID: first.Id, Parameters: json.RawMessage(`{}`)},
			{InstanceID: second.Id, Parameters: json.RawMessage(`{}`)},
		},
	})
	require.NoError(t, err)
	queued, err := ExecuteBatchOperation(ExecuteBatchOperationInput{
		BatchID: planned.BatchId, IdempotencyKey: key, ActorID: 82,
	})
	require.NoError(t, err)
	require.Len(t, queued.Items, 2)
	now := time.Now().Unix()
	require.NoError(t, db.Model(&model.ManagedInstanceOperation{}).
		Where("operation_id = ?", queued.Items[0].Operation.OperationId).
		Updates(map[string]any{"status": model.ManagedInstanceOperationStatusUnknown, "error_code": "remote_result_unknown", "finished_at": now}).Error)
	require.NoError(t, db.Model(&model.ManagedInstanceOperation{}).
		Where("operation_id = ?", queued.Items[1].Operation.OperationId).
		Updates(map[string]any{"status": model.ManagedInstanceOperationStatusSucceeded, "finished_at": now}).Error)

	view, err := GetBatchOperation(planned.BatchId)
	require.NoError(t, err)
	require.Equal(t, model.ManagedInstanceBatchStatusNeedsReconcile, view.Status)
	require.Equal(t, BatchOperationSummary{Total: 2, Succeeded: 1, Unknown: 1}, view.Summary)
	require.NotZero(t, view.FinishedAt)
}
