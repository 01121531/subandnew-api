package managedinstance

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxBatchOperationTargets = 50

var ErrBatchOperationNotFound = errors.New("managed instance batch operation not found")

type BatchOperationTargetInput struct {
	InstanceID int64
	Parameters json.RawMessage
}

type PlanBatchOperationInput struct {
	Action         string
	IdempotencyKey string
	Targets        []BatchOperationTargetInput
	ActorID        int
}

type ExecuteBatchOperationInput struct {
	BatchID        string
	IdempotencyKey string
	ActorID        int
}

type BatchOperationSummary struct {
	Total     int `json:"total"`
	Planned   int `json:"planned"`
	Active    int `json:"active"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Unknown   int `json:"unknown"`
}

type BatchOperationItemView struct {
	InstanceID int64          `json:"instance_id"`
	Position   int            `json:"position"`
	Status     string         `json:"status"`
	ErrorCode  string         `json:"error_code,omitempty"`
	Parameters any            `json:"parameters"`
	Operation  *OperationView `json:"operation,omitempty"`
}

type BatchOperationView struct {
	*model.ManagedInstanceOperationBatch
	Summary          BatchOperationSummary    `json:"summary"`
	Items            []BatchOperationItemView `json:"items"`
	IdempotentReplay bool                     `json:"idempotent_replay,omitempty"`
}

type normalizedBatchTarget struct {
	InstanceID int64           `json:"instance_id"`
	Parameters json.RawMessage `json:"parameters"`
}

func PlanBatchOperation(input PlanBatchOperationInput) (*BatchOperationView, error) {
	input.Action = strings.TrimSpace(input.Action)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ActorID <= 0 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 {
		return nil, ErrInvalidOperation
	}
	targets, err := normalizeBatchTargets(input.Action, input.Targets)
	if err != nil {
		return nil, err
	}
	planHash := batchOperationPlanHash(input.Action, targets)
	batch, replay, err := reserveBatchOperation(input, planHash, len(targets))
	if err != nil {
		return nil, err
	}
	if batch.Status != model.ManagedInstanceBatchStatusPlanning {
		view, viewErr := GetBatchOperation(batch.BatchId)
		if view != nil {
			view.IdempotentReplay = true
		}
		return view, viewErr
	}

	items := make([]model.ManagedInstanceOperationBatchItem, 0, len(targets))
	plannedCount := 0
	for position, target := range targets {
		operation, planErr := PlanOperation(target.InstanceID, PlanOperationInput{
			Action:         input.Action,
			IdempotencyKey: batchChildIdempotencyKey(input.ActorID, input.IdempotencyKey, target.InstanceID),
			Parameters:     target.Parameters,
			ActorID:        input.ActorID,
		})
		item := model.ManagedInstanceOperationBatchItem{
			BatchId: batch.BatchId, InstanceId: target.InstanceID, Position: position,
			Status: model.ManagedInstanceOperationStatusFailed, Parameters: string(target.Parameters),
		}
		if planErr != nil {
			item.ErrorCode = batchOperationErrorCode(planErr)
		} else {
			item.OperationId = operation.OperationId
			item.Status = operation.Status
			plannedCount++
		}
		items = append(items, item)
	}
	status := model.ManagedInstanceBatchStatusPlanned
	if plannedCount == 0 {
		status = model.ManagedInstanceBatchStatusFailed
	} else if plannedCount != len(items) {
		status = model.ManagedInstanceBatchStatusPartiallyPlanned
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.ManagedInstanceOperationBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", batch.Id).First(&current).Error; err != nil {
			return err
		}
		if current.Status != model.ManagedInstanceBatchStatusPlanning {
			return nil
		}
		if err := tx.Create(&items).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ManagedInstanceOperationBatch{}).Where("id = ?", batch.Id).Updates(map[string]any{
			"status": status, "updated_at": common.GetTimestamp(),
		}).Error; err != nil {
			return err
		}
		for _, item := range items {
			outcome := "succeeded"
			if item.ErrorCode != "" {
				outcome = "failed"
			}
			if err := writeAuditOutcome(tx, item.InstanceId, input.ActorID, "batch_operation_plan", outcome, map[string]any{
				"batch_id": batch.BatchId, "operation_id": item.OperationId, "action": input.Action,
				"position": item.Position, "error_code": item.ErrorCode,
				"idempotency_fingerprint": batch.IdempotencyFingerprint,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	view, err := GetBatchOperation(batch.BatchId)
	if view != nil {
		view.IdempotentReplay = replay
	}
	return view, err
}

func reserveBatchOperation(input PlanBatchOperationInput, planHash string, targetCount int) (*model.ManagedInstanceOperationBatch, bool, error) {
	existing, err := findBatchByIdempotency(input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		if !sameSecret(existing.PlanHash, planHash) {
			return nil, false, ErrIdempotencyConflict
		}
		return existing, true, nil
	}
	batchID, err := generateBatchOperationID()
	if err != nil {
		return nil, false, err
	}
	batch := &model.ManagedInstanceOperationBatch{
		BatchId: batchID, ActorId: input.ActorID, Action: input.Action,
		Status: model.ManagedInstanceBatchStatusPlanning, TargetCount: targetCount,
		IdempotencyKey:         idempotencyDigest(input.IdempotencyKey),
		IdempotencyFingerprint: idempotencyFingerprint(input.IdempotencyKey), PlanHash: planHash,
	}
	createErr := model.DB.Create(batch).Error
	if createErr == nil {
		return batch, false, nil
	}
	existing, lookupErr := findBatchByIdempotency(input.ActorID, input.IdempotencyKey)
	if lookupErr != nil || existing == nil {
		return nil, false, createErr
	}
	if !sameSecret(existing.PlanHash, planHash) {
		return nil, false, ErrIdempotencyConflict
	}
	return existing, true, nil
}

func ExecuteBatchOperation(input ExecuteBatchOperationInput) (*BatchOperationView, error) {
	input.BatchID = strings.TrimSpace(input.BatchID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ActorID <= 0 || input.BatchID == "" || input.IdempotencyKey == "" {
		return nil, ErrInvalidOperation
	}
	batch, err := getBatchModel(input.BatchID)
	if err != nil {
		return nil, err
	}
	if !sameSecret(batch.IdempotencyKey, idempotencyDigest(input.IdempotencyKey)) {
		return nil, ErrIdempotencyConflict
	}
	if batch.Status == model.ManagedInstanceBatchStatusSucceeded ||
		batch.Status == model.ManagedInstanceBatchStatusPartiallyFailed ||
		batch.Status == model.ManagedInstanceBatchStatusNeedsReconcile ||
		batch.Status == model.ManagedInstanceBatchStatusFailed && batch.ExecutedAt > 0 {
		view, viewErr := GetBatchOperation(batch.BatchId)
		if view != nil {
			view.IdempotentReplay = true
		}
		return view, viewErr
	}

	now := common.GetTimestamp()
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.ManagedInstanceOperationBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", batch.Id).First(&current).Error; err != nil {
			return err
		}
		if current.ExecutedAt != 0 {
			return nil
		}
		if current.Status != model.ManagedInstanceBatchStatusPlanned &&
			current.Status != model.ManagedInstanceBatchStatusPartiallyPlanned {
			return ErrOperationNotExecutable
		}
		return tx.Model(&model.ManagedInstanceOperationBatch{}).Where("id = ?", current.Id).Updates(map[string]any{
			"status": model.ManagedInstanceBatchStatusQueued, "executed_by": input.ActorID,
			"executed_at": now, "updated_at": now,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	batch, err = getBatchModel(input.BatchID)
	if err != nil {
		return nil, err
	}
	if err := enqueueBatchOperations(batch, batch.ExecutedBy); err != nil {
		return nil, err
	}
	return GetBatchOperation(batch.BatchId)
}

func ResumeBatchOperation(batchID string) (*BatchOperationView, error) {
	batch, err := getBatchModel(strings.TrimSpace(batchID))
	if err != nil {
		return nil, err
	}
	if batch.ExecutedAt == 0 {
		return nil, ErrOperationNotExecutable
	}
	actorID := batch.ExecutedBy
	if actorID == 0 {
		actorID = batch.ActorId
	}
	if err := enqueueBatchOperations(batch, actorID); err != nil {
		return nil, err
	}
	return GetBatchOperation(batch.BatchId)
}

func enqueueBatchOperations(batch *model.ManagedInstanceOperationBatch, actorID int) error {
	var items []model.ManagedInstanceOperationBatchItem
	if err := model.DB.Where("batch_id = ?", batch.BatchId).Order("position asc").Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if item.OperationId == "" || item.ErrorCode != "" {
			continue
		}
		operation, operationErr := getOperationModel(item.InstanceId, item.OperationId)
		if operationErr != nil {
			if err := updateBatchItemFailure(item.Id, "operation_not_found"); err != nil {
				return err
			}
			continue
		}
		if operation.Status != model.ManagedInstanceOperationStatusPlanned {
			continue
		}
		queued, _, executeErr := ExecuteOperation(item.InstanceId, ExecuteOperationInput{
			OperationID:    item.OperationId,
			IdempotencyKey: batchChildIdempotencyKeyFromDigest(batch.ActorId, batch.IdempotencyKey, item.InstanceId),
			ActorID:        actorID, BatchID: batch.BatchId,
		})
		outcome := "queued"
		errorCode := ""
		status := model.ManagedInstanceOperationStatusQueued
		if executeErr != nil {
			current, currentErr := getOperationModel(item.InstanceId, item.OperationId)
			if currentErr == nil && current.Status != model.ManagedInstanceOperationStatusPlanned {
				continue
			} else {
				outcome = "failed"
				errorCode = batchOperationErrorCode(executeErr)
				status = model.ManagedInstanceOperationStatusFailed
			}
		} else if queued != nil {
			if queued.IdempotentReplay {
				continue
			}
			status = queued.Status
		}
		if err := model.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&model.ManagedInstanceOperationBatchItem{}).Where("id = ?", item.Id).Updates(map[string]any{
				"status": status, "error_code": errorCode, "updated_at": common.GetTimestamp(),
			}).Error; err != nil {
				return err
			}
			return writeAuditOutcome(tx, item.InstanceId, actorID, "batch_operation_execute", outcome, map[string]any{
				"batch_id": batch.BatchId, "operation_id": item.OperationId, "action": batch.Action,
				"position": item.Position, "error_code": errorCode,
				"idempotency_fingerprint": batch.IdempotencyFingerprint,
			})
		}); err != nil {
			return err
		}
	}
	return nil
}

func GetBatchOperation(batchID string) (*BatchOperationView, error) {
	batch, err := getBatchModel(strings.TrimSpace(batchID))
	if err != nil {
		return nil, err
	}
	var storedItems []model.ManagedInstanceOperationBatchItem
	if err := model.DB.Where("batch_id = ?", batch.BatchId).Order("position asc").Find(&storedItems).Error; err != nil {
		return nil, err
	}
	view := &BatchOperationView{
		ManagedInstanceOperationBatch: batch,
		Items:                         make([]BatchOperationItemView, 0, len(storedItems)),
	}
	for _, item := range storedItems {
		itemView := BatchOperationItemView{
			InstanceID: item.InstanceId, Position: item.Position, Status: item.Status,
			ErrorCode: item.ErrorCode, Parameters: decodeOperationJSON(item.Parameters),
		}
		if item.OperationId != "" {
			operation, operationErr := getOperationModel(item.InstanceId, item.OperationId)
			if operationErr == nil {
				itemView.Operation = operationView(operation)
				itemView.Status = operation.Status
				if operation.ErrorCode != "" {
					itemView.ErrorCode = operation.ErrorCode
				}
			} else if itemView.ErrorCode == "" {
				itemView.Status = model.ManagedInstanceOperationStatusFailed
				itemView.ErrorCode = "operation_not_found"
			}
		}
		view.Items = append(view.Items, itemView)
	}
	derivedSummary, derivedStatus := summarizeBatchOperation(batch, view.Items)
	view.Summary = derivedSummary
	if derivedStatus != batch.Status || (isTerminalBatchStatus(derivedStatus) && batch.FinishedAt == 0) {
		updates := map[string]any{"status": derivedStatus, "updated_at": common.GetTimestamp()}
		if isTerminalBatchStatus(derivedStatus) && batch.FinishedAt == 0 {
			batch.FinishedAt = common.GetTimestamp()
			updates["finished_at"] = batch.FinishedAt
		}
		if err := model.DB.Model(&model.ManagedInstanceOperationBatch{}).Where("id = ?", batch.Id).Updates(updates).Error; err != nil {
			return nil, err
		}
		batch.Status = derivedStatus
	}
	return view, nil
}

func normalizeBatchTargets(action string, inputs []BatchOperationTargetInput) ([]normalizedBatchTarget, error) {
	if len(inputs) < 2 || len(inputs) > maxBatchOperationTargets {
		return nil, fmt.Errorf("%w: batch targets must contain 2 to %d instances", ErrInvalidOperation, maxBatchOperationTargets)
	}
	seen := make(map[int64]struct{}, len(inputs))
	targets := make([]normalizedBatchTarget, 0, len(inputs))
	for _, input := range inputs {
		if input.InstanceID <= 0 {
			return nil, ErrInvalidOperation
		}
		if _, exists := seen[input.InstanceID]; exists {
			return nil, fmt.Errorf("%w: duplicate instance target", ErrInvalidOperation)
		}
		seen[input.InstanceID] = struct{}{}
		parameters, _, err := normalizeOperationParameters(action, input.Parameters)
		if err != nil {
			return nil, err
		}
		targets = append(targets, normalizedBatchTarget{InstanceID: input.InstanceID, Parameters: parameters})
	}
	sort.Slice(targets, func(left, right int) bool { return targets[left].InstanceID < targets[right].InstanceID })
	return targets, nil
}

func summarizeBatchOperation(batch *model.ManagedInstanceOperationBatch, items []BatchOperationItemView) (BatchOperationSummary, string) {
	summary := BatchOperationSummary{Total: len(items)}
	for _, item := range items {
		switch item.Status {
		case model.ManagedInstanceOperationStatusPlanned:
			if item.ErrorCode == "" {
				summary.Planned++
			} else {
				summary.Failed++
			}
		case model.ManagedInstanceOperationStatusQueued, model.ManagedInstanceOperationStatusRunning:
			summary.Active++
		case model.ManagedInstanceOperationStatusSucceeded:
			summary.Succeeded++
		case model.ManagedInstanceOperationStatusUnknown:
			summary.Unknown++
		default:
			summary.Failed++
		}
	}
	if batch.ExecutedAt == 0 {
		return summary, batch.Status
	}
	if summary.Active > 0 || summary.Planned > 0 {
		for _, item := range items {
			if item.Status == model.ManagedInstanceOperationStatusRunning {
				return summary, model.ManagedInstanceBatchStatusRunning
			}
		}
		return summary, model.ManagedInstanceBatchStatusQueued
	}
	if summary.Unknown > 0 {
		return summary, model.ManagedInstanceBatchStatusNeedsReconcile
	}
	if summary.Succeeded == summary.Total {
		return summary, model.ManagedInstanceBatchStatusSucceeded
	}
	if summary.Failed == summary.Total {
		return summary, model.ManagedInstanceBatchStatusFailed
	}
	return summary, model.ManagedInstanceBatchStatusPartiallyFailed
}

func findBatchByIdempotency(actorID int, key string) (*model.ManagedInstanceOperationBatch, error) {
	var batch model.ManagedInstanceOperationBatch
	err := model.DB.Where("actor_id = ? AND idempotency_key = ?", actorID, idempotencyDigest(key)).First(&batch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &batch, err
}

func getBatchModel(batchID string) (*model.ManagedInstanceOperationBatch, error) {
	if batchID == "" {
		return nil, ErrBatchOperationNotFound
	}
	var batch model.ManagedInstanceOperationBatch
	if err := model.DB.Where("batch_id = ?", batchID).First(&batch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBatchOperationNotFound
		}
		return nil, err
	}
	return &batch, nil
}

func batchOperationPlanHash(action string, targets []normalizedBatchTarget) string {
	encoded, _ := json.Marshal(struct {
		Action  string                  `json:"action"`
		Targets []normalizedBatchTarget `json:"targets"`
	}{Action: action, Targets: targets})
	return idempotencyDigest(string(encoded))
}

func batchChildIdempotencyKey(actorID int, key string, instanceID int64) string {
	return batchChildIdempotencyKeyFromDigest(actorID, idempotencyDigest(key), instanceID)
}

func batchChildIdempotencyKeyFromDigest(actorID int, parentDigest string, instanceID int64) string {
	return idempotencyDigest(fmt.Sprintf("managed-instance-batch:%d:%s:%d", actorID, parentDigest, instanceID))
}

func generateBatchOperationID() (string, error) {
	key, err := common.GenerateRandomCharsKey(24)
	if err != nil {
		return "", err
	}
	return "mibatch_" + key, nil
}

func batchOperationErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInstanceNotFound):
		return "instance_not_found"
	case errors.Is(err, ErrInvalidOperation):
		return "invalid_operation"
	case errors.Is(err, ErrUnsupportedCapability):
		return "unsupported_capability"
	case errors.Is(err, ErrObserveModeWrite):
		return "observe_mode_write_forbidden"
	case errors.Is(err, ErrIdempotencyConflict):
		return "idempotency_conflict"
	case errors.Is(err, ErrOperationBusy):
		return "operation_busy"
	case errors.Is(err, ErrOperationNotExecutable):
		return "operation_not_executable"
	default:
		return managedInstanceOperationErrorCode(err)
	}
}

func updateBatchItemFailure(itemID int64, errorCode string) error {
	return model.DB.Model(&model.ManagedInstanceOperationBatchItem{}).Where("id = ?", itemID).Updates(map[string]any{
		"status":     model.ManagedInstanceOperationStatusFailed,
		"error_code": errorCode,
		"updated_at": common.GetTimestamp(),
	}).Error
}

func decodeOperationJSON(raw string) any {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return map[string]any{}
	}
	return value
}

func isTerminalBatchStatus(status string) bool {
	return status == model.ManagedInstanceBatchStatusSucceeded ||
		status == model.ManagedInstanceBatchStatusPartiallyFailed ||
		status == model.ManagedInstanceBatchStatusNeedsReconcile ||
		status == model.ManagedInstanceBatchStatusFailed
}
