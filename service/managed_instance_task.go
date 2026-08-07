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
)

const maxManagedInstanceProbeBackoff = 30 * time.Minute

type ManagedInstanceProbePayload struct {
	InstanceID int64 `json:"instance_id"`
	ActorID    int   `json:"actor_id,omitempty"`
}

type managedInstanceProbeHandler struct{}

var (
	managedInstanceProbeSlotsOnce sync.Once
	managedInstanceProbeSlots     chan struct{}
)

func init() {
	RegisterSystemTaskHandler(managedInstanceProbeHandler{})
	RegisterSystemTaskHandler(managedInstanceOperationHandler{})
}

func (managedInstanceProbeHandler) Type() string {
	return model.SystemTaskTypeManagedInstanceProbe
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
	operation, err := managedinstance.RunOperation(ctx, payload.OperationID, task.TaskID)
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

	result, err := managedinstance.Probe(ctx, payload.InstanceID, payload.ActorID)
	if err != nil {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, managedInstanceProbeErrorCode(err))
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, "")
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
	var instances []*model.ManagedInstance
	if err := model.DB.Order("last_checked_at asc, id asc").Limit(500).Find(&instances).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("managed instance probe scheduler query failed: %v", err))
		return
	}
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
