package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/logger"
	"github.com/01121531/HUICHUAN-AI/model"
	"github.com/01121531/HUICHUAN-AI/service/managedinstance"
	"gorm.io/gorm"
)

const maxManagedInstanceProbeBackoff = 30 * time.Minute

const managedInstanceSyncInterval = 5 * time.Minute

type ManagedInstanceProbePayload struct {
	InstanceID int64 `json:"instance_id"`
	ActorID    int   `json:"actor_id,omitempty"`
}

type managedInstanceProbeHandler struct{}

type ManagedInstanceSyncPayload struct {
	InstanceID int64 `json:"instance_id"`
	ActorID    int   `json:"actor_id,omitempty"`
}

type managedInstanceSyncHandler struct{}

var (
	managedInstanceProbeSlotsOnce sync.Once
	managedInstanceProbeSlots     chan struct{}
)

func init() {
	RegisterSystemTaskHandler(managedInstanceProbeHandler{})
	RegisterSystemTaskHandler(managedInstanceSyncHandler{})
	RegisterSystemTaskHandler(managedInstanceOperationHandler{})
}

func (managedInstanceProbeHandler) Type() string {
	return model.SystemTaskTypeManagedInstanceProbe
}

func (managedInstanceSyncHandler) Type() string {
	return model.SystemTaskTypeManagedInstanceSync
}

type ManagedInstanceOperationPayload struct {
	OperationID string `json:"operation_id"`
	InstanceID  int64  `json:"instance_id"`
	ActorID     int    `json:"actor_id"`
}

type managedInstanceOperationHandler struct{}

func (managedInstanceOperationHandler) Type() string {
	return model.SystemTaskTypeManagedInstanceOperation
}

func (managedInstanceOperationHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := ManagedInstanceOperationPayload{}
	if err := task.DecodePayload(&payload); err != nil || payload.OperationID == "" || payload.InstanceID <= 0 || task.ScopeKey != strconv.FormatInt(payload.InstanceID, 10) {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "invalid_operation_payload")
		return
	}
	operation, err := managedinstance.RunOperationWithLease(ctx, payload.OperationID, task.TaskID, runnerID)
	if err != nil {
		errorCode := "managed_instance_operation_failed"
		var executionError *managedinstance.OperationExecutionError
		if errors.As(err, &executionError) {
			errorCode = executionError.Code
		}
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, errorCode)
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, operation, "")
}

func (managedInstanceProbeHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := ManagedInstanceProbePayload{}
	if err := task.DecodePayload(&payload); err != nil || payload.InstanceID <= 0 || task.ScopeKey != strconv.FormatInt(payload.InstanceID, 10) {
		if err == nil {
			err = errors.New("managed instance probe payload is invalid")
		}
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, err.Error())
		return
	}

	slots := getManagedInstanceProbeSlots()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "probe cancelled")
		return
	}

	guard := managedInstanceTaskCommitGuard(task.TaskID, runnerID)
	result, err := managedinstance.ProbeWithCommitGuard(ctx, payload.InstanceID, payload.ActorID, guard)
	if err != nil {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, managedInstanceProbeErrorCode(err))
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, "")
}

func (managedInstanceSyncHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := ManagedInstanceSyncPayload{}
	if err := task.DecodePayload(&payload); err != nil || payload.InstanceID <= 0 || task.ScopeKey != strconv.FormatInt(payload.InstanceID, 10) {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "invalid_sync_payload")
		return
	}

	slots := getManagedInstanceProbeSlots()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "sync_cancelled")
		return
	}

	guard := managedInstanceTaskCommitGuard(task.TaskID, runnerID)
	inventory, err := managedinstance.CollectInventoryWithCommitGuard(ctx, payload.InstanceID, "auto", "", guard)
	if err != nil {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, managedInstanceProbeErrorCode(err))
		return
	}
	summary, err := managedinstance.CollectSummaryWithCommitGuard(ctx, payload.InstanceID, managedinstance.TimeWindow{}, guard)
	if err != nil {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, managedInstanceProbeErrorCode(err))
		return
	}
	result := map[string]any{"inventory": inventory, "summary": summary}
	if inventory.CollectionStatus != model.ManagedInstanceCollectionSucceeded {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, result, inventory.ErrorCode)
		return
	}
	if summary.CollectionStatus != model.ManagedInstanceCollectionSucceeded {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, result, summary.ErrorCode)
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, "")
}

func managedInstanceTaskCommitGuard(taskID string, runnerID string) managedinstance.CommitGuard {
	return func(tx *gorm.DB) error {
		return model.RequireValidSystemTaskLease(tx, taskID, runnerID, common.GetTimestamp())
	}
}

func getManagedInstanceProbeSlots() chan struct{} {
	managedInstanceProbeSlotsOnce.Do(func() {
		limit := common.GetEnvOrDefault("MANAGED_INSTANCE_PROBE_MAX_CONCURRENCY", 8)
		if limit < 1 {
			limit = 1
		}
		if limit > 64 {
			limit = 64
		}
		managedInstanceProbeSlots = make(chan struct{}, limit)
	})
	return managedInstanceProbeSlots
}

func scheduleDueManagedInstanceProbes(now int64) {
	forEachManagedInstanceBatch(func(instances []*model.ManagedInstance) bool {
		for _, instance := range instances {
			if !managedInstanceProbeDue(instance, now) {
				continue
			}
			if _, _, err := EnqueueScopedSystemTask(
				model.SystemTaskTypeManagedInstanceProbe,
				strconv.FormatInt(instance.Id, 10),
				ManagedInstanceProbePayload{InstanceID: instance.Id},
				nil,
			); err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("managed instance probe scheduler enqueue failed: instance=%d err=%v", instance.Id, err))
			}
		}
		return true
	})
}

func scheduleDueManagedInstanceSyncs(now int64) {
	forEachManagedInstanceBatch(func(instances []*model.ManagedInstance) bool {
		latest := make(map[int64]int64, len(instances))
		ids := make([]int64, 0, len(instances))
		for _, instance := range instances {
			ids = append(ids, instance.Id)
		}
		var snapshots []model.ManagedInstanceSnapshot
		if err := model.DB.Select("instance_id", "MAX(observed_at) AS observed_at").
			Where("instance_id IN ? AND snapshot_type = ?", ids, model.ManagedInstanceSnapshotTypeSummary).
			Group("instance_id").Find(&snapshots).Error; err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed instance sync snapshot query failed: %v", err))
			return false
		}
		for _, snapshot := range snapshots {
			latest[snapshot.InstanceId] = snapshot.ObservedAt
		}
		intervalSeconds := int64(managedInstanceSyncInterval / time.Second)
		for _, instance := range instances {
			jitter := instance.Id % (intervalSeconds/5 + 1)
			if observedAt := latest[instance.Id]; observedAt > 0 && now < observedAt+intervalSeconds+jitter {
				continue
			}
			if _, _, err := EnqueueScopedSystemTask(
				model.SystemTaskTypeManagedInstanceSync,
				strconv.FormatInt(instance.Id, 10),
				ManagedInstanceSyncPayload{InstanceID: instance.Id},
				nil,
			); err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("managed instance sync scheduler enqueue failed: instance=%d err=%v", instance.Id, err))
			}
		}
		return true
	})
}

func forEachManagedInstanceBatch(visit func([]*model.ManagedInstance) bool) {
	const batchSize = 500
	var lastID int64
	for {
		var instances []*model.ManagedInstance
		if err := model.DB.Where("id > ?", lastID).Order("id asc").Limit(batchSize).Find(&instances).Error; err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed instance scheduler query failed: %v", err))
			return
		}
		if len(instances) == 0 {
			return
		}
		if !visit(instances) {
			return
		}
		lastID = instances[len(instances)-1].Id
		if len(instances) < batchSize {
			return
		}
	}
}

func managedInstanceProbeDue(instance *model.ManagedInstance, now int64) bool {
	if instance == nil || instance.Id <= 0 || instance.LastCheckedAt == 0 {
		return instance != nil && instance.Id > 0
	}
	interval := time.Duration(instance.CheckIntervalSeconds) * time.Second
	if interval < 10*time.Second {
		interval = time.Minute
	}
	failures := instance.ConsecutiveFailures
	if failures > 0 {
		if failures > 6 {
			failures = 6
		}
		interval *= time.Duration(1 << failures)
		if interval > maxManagedInstanceProbeBackoff {
			interval = maxManagedInstanceProbeBackoff
		}
	}
	jitterWindow := int64(interval / 5 / time.Second)
	jitter := int64(0)
	if jitterWindow > 0 {
		jitter = instance.Id % (jitterWindow + 1)
	}
	return now >= instance.LastCheckedAt+int64(interval/time.Second)+jitter
}

func managedInstanceProbeErrorCode(err error) string {
	var probeError *managedinstance.ProbeError
	if errors.As(err, &probeError) {
		return probeError.Code
	}
	switch {
	case errors.Is(err, managedinstance.ErrInstanceNotFound):
		return "instance_not_found"
	case errors.Is(err, managedinstance.ErrCredentialKeyNotConfigured):
		return "credential_key_not_configured"
	default:
		return "probe_failed"
	}
}
