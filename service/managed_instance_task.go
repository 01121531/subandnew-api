package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/logger"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
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

type ManagedUsageExportPayload struct {
	InstanceID int64      `json:"instance_id"`
	ActorID    int        `json:"actor_id"`
	Query      url.Values `json:"query"`
}

type managedUsageExportHandler struct{}

type ManagedUsageExportView struct {
	ID            int64      `json:"id"`
	TaskID        string     `json:"task_id"`
	InstanceID    int64      `json:"instance_id"`
	InstanceName  string     `json:"instance_name"`
	InstanceKind  string     `json:"instance_kind"`
	ActorID       int        `json:"actor_id"`
	ActorName     string     `json:"actor_name"`
	Filters       url.Values `json:"filters"`
	Status        string     `json:"status"`
	QueuePosition int64      `json:"queue_position"`
	Progress      int        `json:"progress"`
	Processed     int64      `json:"processed"`
	Total         int64      `json:"total"`
	FileName      string     `json:"file_name"`
	FileSize      int64      `json:"file_size"`
	RecordCount   int        `json:"record_count"`
	ErrorCode     string     `json:"error_code"`
	StartedAt     int64      `json:"started_at"`
	FinishedAt    int64      `json:"finished_at"`
	ExpiresAt     int64      `json:"expires_at"`
	CreatedAt     int64      `json:"created_at"`
	UpdatedAt     int64      `json:"updated_at"`
}

type ManagedUsageExportListView struct {
	Items     []*ManagedUsageExportView `json:"items"`
	Total     int64                     `json:"total"`
	Page      int                       `json:"page"`
	PageSize  int                       `json:"page_size"`
	HasActive bool                      `json:"has_active"`
}

type managedInstanceOperationScopedSlot struct {
	slots chan struct{}
	users int
}

var (
	managedInstanceProbeSlotsOnce      sync.Once
	managedInstanceProbeSlots          chan struct{}
	managedInstanceOperationSlotsOnce  sync.Once
	managedInstanceOperationSlots      chan struct{}
	managedInstanceOperationSlotsMu    sync.Mutex
	managedInstanceOperationHostSlots  = map[string]*managedInstanceOperationScopedSlot{}
	managedInstanceOperationBatchSlots = map[string]*managedInstanceOperationScopedSlot{}
)

func init() {
	RegisterSystemTaskHandler(managedInstanceProbeHandler{})
	RegisterSystemTaskHandler(managedInstanceSyncHandler{})
	RegisterSystemTaskHandler(managedInstanceOperationHandler{})
	RegisterSystemTaskHandler(managedUsageExportHandler{})
}

func (managedUsageExportHandler) Type() string {
	return model.SystemTaskTypeManagedUsageExport
}

func EnqueueManagedUsageExport(instanceID int64, actorID int, query url.Values) (*model.SystemTask, error) {
	if instanceID <= 0 || actorID <= 0 {
		return nil, managedinstance.ErrInvalidInstance
	}
	instance, err := managedinstance.Get(instanceID)
	if err != nil {
		return nil, err
	}
	actorName := fmt.Sprintf("#%d", actorID)
	if actor, actorErr := model.GetUserById(actorID, false); actorErr == nil && actor != nil {
		actorName = actor.Username
		if actor.DisplayName != "" {
			actorName = actor.DisplayName
		}
	}
	querySnapshot := cloneManagedUsageExportQuery(query)
	queryJSON, err := json.Marshal(querySnapshot)
	if err != nil {
		return nil, err
	}
	payload := ManagedUsageExportPayload{InstanceID: instanceID, ActorID: actorID, Query: querySnapshot}
	state := managedinstance.UsageRecordExportProgress{Progress: 0, Processed: 0, Total: 0, Stage: "queued"}
	task, err := model.CreateManagedUsageExport(&model.ManagedUsageExport{
		InstanceID: instanceID, InstanceName: instance.Name, InstanceKind: instance.Kind,
		ActorID: actorID, ActorName: actorName, Query: string(queryJSON),
	}, payload, state)
	if err == nil {
		notifySystemTaskRunner()
	}
	return task, err
}

func cloneManagedUsageExportQuery(query url.Values) url.Values {
	copy := make(url.Values, len(query))
	for key, values := range query {
		copy[key] = append([]string(nil), values...)
	}
	copy.Del("p")
	copy.Del("page")
	copy.Del("page_size")
	return copy
}

func managedUsageExportView(record *model.ManagedUsageExport) *ManagedUsageExportView {
	if record == nil {
		return nil
	}
	filters := url.Values{}
	_ = json.Unmarshal([]byte(record.Query), &filters)
	return &ManagedUsageExportView{
		ID: record.ID, TaskID: record.TaskID, InstanceID: record.InstanceID,
		InstanceName: record.InstanceName, InstanceKind: record.InstanceKind,
		ActorID: record.ActorID, ActorName: record.ActorName, Filters: filters,
		Status: record.Status, QueuePosition: model.ManagedUsageExportQueuePosition(record),
		Progress: record.Progress, Processed: record.Processed, Total: record.Total,
		FileName: record.FileName, FileSize: record.FileSize, RecordCount: record.RecordCount,
		ErrorCode: record.ErrorCode, StartedAt: record.StartedAt, FinishedAt: record.FinishedAt,
		ExpiresAt: record.ExpiresAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func ListManagedUsageExports(filter model.ManagedUsageExportListFilter) (*ManagedUsageExportListView, error) {
	list, err := model.ListManagedUsageExports(filter)
	if err != nil {
		return nil, err
	}
	items := make([]*ManagedUsageExportView, 0, len(list.Items))
	for _, record := range list.Items {
		items = append(items, managedUsageExportView(record))
	}
	return &ManagedUsageExportListView{
		Items: items, Total: list.Total, Page: list.Page, PageSize: list.PageSize, HasActive: list.HasActive,
	}, nil
}

func GetManagedUsageExport(taskID string, actorID int, root bool) (*model.ManagedUsageExport, error) {
	record, err := model.GetManagedUsageExport(taskID)
	if err != nil || record == nil {
		return record, err
	}
	if !root && record.ActorID != actorID {
		return nil, nil
	}
	return record, nil
}

func GetManagedUsageExportView(taskID string, actorID int, root bool) (*ManagedUsageExportView, error) {
	record, err := GetManagedUsageExport(taskID, actorID, root)
	return managedUsageExportView(record), err
}

func CleanupExpiredManagedUsageExports() error {
	taskIDs, err := model.ExpireManagedUsageExports(common.GetTimestamp())
	if err != nil {
		return err
	}
	for _, taskID := range taskIDs {
		managedinstance.RemoveUsageRecordExportArtifact(taskID)
	}
	managedinstance.CleanupStaleUsageRecordExportParts()
	return nil
}

func CancelManagedUsageExport(taskID string, actorID int, root bool) error {
	return model.CancelManagedUsageExport(taskID, actorID, root)
}

func RetryManagedUsageExport(taskID string, actorID int, root bool) (*model.SystemTask, error) {
	record, err := GetManagedUsageExport(taskID, actorID, root)
	if err != nil || record == nil {
		return nil, err
	}
	if record.Status != model.ManagedUsageExportStatusFailed && record.Status != model.ManagedUsageExportStatusExpired {
		return nil, model.ErrManagedUsageExportConflict
	}
	query := url.Values{}
	if err := json.Unmarshal([]byte(record.Query), &query); err != nil {
		return nil, err
	}
	return EnqueueManagedUsageExport(record.InstanceID, actorID, query)
}

func GetManagedUsageExportTask(taskID string, instanceID int64, actorID int) (*model.SystemTask, error) {
	task, err := model.GetSystemTaskByTaskID(taskID)
	if err != nil || task == nil {
		return task, err
	}
	payload := ManagedUsageExportPayload{}
	if task.Type != model.SystemTaskTypeManagedUsageExport || task.DecodePayload(&payload) != nil || payload.InstanceID != instanceID || payload.ActorID != actorID {
		return nil, nil
	}
	return task, nil
}

func (managedUsageExportHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := ManagedUsageExportPayload{}
	if err := task.DecodePayload(&payload); err != nil || payload.InstanceID <= 0 || payload.ActorID <= 0 || task.ScopeKey != "" {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "invalid_usage_export_payload")
		return
	}
	if err := model.StartManagedUsageExport(task.TaskID); err != nil {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "usage_export_status_conflict")
		return
	}
	artifact, err := managedinstance.ExportUsageRecordsCSVToTaskFile(
		ctx,
		payload.InstanceID,
		task.TaskID,
		payload.Query,
		func(progress managedinstance.UsageRecordExportProgress) error {
			if err := model.UpdateManagedUsageExportProgress(task.TaskID, progress.Progress, progress.Processed, progress.Total); err != nil {
				return err
			}
			return model.UpdateSystemTaskState(task.TaskID, runnerID, progress)
		},
	)
	if err != nil {
		if ctx.Err() != nil {
			managedinstance.RemoveUsageRecordExportArtifact(task.TaskID)
			if model.RequeueManagedUsageExport(task.TaskID, runnerID) == nil {
				notifySystemTaskRunner()
			}
			return
		}
		managedinstance.RecordUsageRecordExportAudit(payload.InstanceID, payload.ActorID, 0, err)
		errorCode := "usage_export_failed"
		if errors.Is(err, managedinstance.ErrUsageExportTooLarge) {
			errorCode = "usage_export_too_large"
		} else if errors.Is(err, managedinstance.ErrUsageExportIncomplete) {
			errorCode = "usage_export_incomplete"
		}
		if model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, errorCode) == nil {
			_ = model.FinishManagedUsageExport(task.TaskID, model.ManagedUsageExportStatusFailed, "", 0, 0, errorCode, 0)
		}
		return
	}
	managedinstance.RecordUsageRecordExportAudit(payload.InstanceID, payload.ActorID, artifact.RecordCount, nil)
	if model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, artifact, "") == nil {
		_ = model.FinishManagedUsageExport(task.TaskID, model.ManagedUsageExportStatusSucceeded, artifact.FileName, artifact.Size, artifact.RecordCount, "", artifact.ExpiresAt)
	}
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
	BatchID     string `json:"batch_id,omitempty"`
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
	releaseSlots, err := acquireManagedInstanceOperationSlots(ctx, payload)
	if err != nil {
		_ = managedinstance.FailQueuedOperation(payload.OperationID, task.TaskID, payload.ActorID, "operation_cancelled")
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "operation_cancelled")
		return
	}
	defer releaseSlots()
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

func acquireManagedInstanceOperationSlots(ctx context.Context, payload ManagedInstanceOperationPayload) (func(), error) {
	releases := make([]func(), 0, 3)
	releaseAll := func() {
		for index := len(releases) - 1; index >= 0; index-- {
			releases[index]()
		}
	}

	instance, err := managedinstance.Get(payload.InstanceID)
	if err != nil {
		releaseAll()
		return nil, err
	}
	if payload.BatchID != "" {
		batch := retainManagedInstanceOperationScopedSlot(
			managedInstanceOperationBatchSlots,
			payload.BatchID,
			common.GetEnvOrDefault("MANAGED_INSTANCE_BATCH_MAX_CONCURRENCY", 2),
		)
		if err := acquireManagedInstanceOperationSlot(ctx, batch.slots); err != nil {
			releaseManagedInstanceOperationScopedSlot(managedInstanceOperationBatchSlots, payload.BatchID, batch, false)
			releaseAll()
			return nil, err
		}
		releases = append(releases, func() {
			releaseManagedInstanceOperationScopedSlot(managedInstanceOperationBatchSlots, payload.BatchID, batch, true)
		})
	}

	hostKey := strconv.FormatInt(payload.InstanceID, 10)
	if parsed, parseErr := url.Parse(instance.BaseURL); parseErr == nil && parsed.Hostname() != "" {
		hostKey = strings.ToLower(parsed.Hostname())
	}
	host := retainManagedInstanceOperationScopedSlot(
		managedInstanceOperationHostSlots,
		hostKey,
		common.GetEnvOrDefault("MANAGED_INSTANCE_OPERATION_MAX_PER_HOST", 2),
	)
	if err := acquireManagedInstanceOperationSlot(ctx, host.slots); err != nil {
		releaseManagedInstanceOperationScopedSlot(managedInstanceOperationHostSlots, hostKey, host, false)
		releaseAll()
		return nil, err
	}
	releases = append(releases, func() {
		releaseManagedInstanceOperationScopedSlot(managedInstanceOperationHostSlots, hostKey, host, true)
	})

	global := getManagedInstanceOperationSlots()
	if err := acquireManagedInstanceOperationSlot(ctx, global); err != nil {
		releaseAll()
		return nil, err
	}
	releases = append(releases, func() { <-global })
	return releaseAll, nil
}

func getManagedInstanceOperationSlots() chan struct{} {
	managedInstanceOperationSlotsOnce.Do(func() {
		limit := boundedManagedInstanceOperationConcurrency(
			common.GetEnvOrDefault("MANAGED_INSTANCE_OPERATION_MAX_CONCURRENCY", 4),
		)
		managedInstanceOperationSlots = make(chan struct{}, limit)
	})
	return managedInstanceOperationSlots
}

func retainManagedInstanceOperationScopedSlot(store map[string]*managedInstanceOperationScopedSlot, key string, configured int) *managedInstanceOperationScopedSlot {
	limit := boundedManagedInstanceOperationConcurrency(configured)
	managedInstanceOperationSlotsMu.Lock()
	defer managedInstanceOperationSlotsMu.Unlock()
	entry := store[key]
	if entry == nil {
		entry = &managedInstanceOperationScopedSlot{slots: make(chan struct{}, limit)}
		store[key] = entry
	}
	entry.users++
	return entry
}

func releaseManagedInstanceOperationScopedSlot(store map[string]*managedInstanceOperationScopedSlot, key string, entry *managedInstanceOperationScopedSlot, acquired bool) {
	if acquired {
		<-entry.slots
	}
	managedInstanceOperationSlotsMu.Lock()
	defer managedInstanceOperationSlotsMu.Unlock()
	entry.users--
	if entry.users == 0 && store[key] == entry {
		delete(store, key)
	}
}

func boundedManagedInstanceOperationConcurrency(limit int) int {
	if limit < 1 {
		return 1
	}
	if limit > 32 {
		return 32
	}
	return limit
}

func acquireManagedInstanceOperationSlot(ctx context.Context, slots chan struct{}) error {
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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

func resumeManagedInstanceOperationBatches() {
	var batches []model.ManagedInstanceOperationBatch
	if err := model.DB.Select("batch_id").
		Where("executed_at > 0 AND status IN ?", []string{
			model.ManagedInstanceBatchStatusQueued,
			model.ManagedInstanceBatchStatusRunning,
		}).
		Order("updated_at asc").Limit(100).Find(&batches).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("managed instance batch recovery query failed: %v", err))
		return
	}
	for _, batch := range batches {
		if _, err := managedinstance.ResumeBatchOperation(batch.BatchId); err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed instance batch recovery failed: batch=%s err=%v", batch.BatchId, err))
		}
	}
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
