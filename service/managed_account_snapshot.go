package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"gorm.io/gorm/clause"
)

const (
	managedAccountSyncInterval      = time.Hour
	managedAccountRefreshCooldown   = 5 * time.Minute
	managedAccountCustomRetention   = 30 * 24 * time.Hour
	managedAccountDefaultTimezone   = "Asia/Shanghai"
	managedAccountInventoryRangeKey = "inventory"
	managedAccountSnapshotSchema    = 1
	managedAccountStandardTaskMode  = "standard"
	managedAccountCustomTaskMode    = "custom"
)

var managedAccountPresetDays = [...]int{1, 7, 14, 30}

type ManagedAccountRange struct {
	RangeKey   string `json:"range_key"`
	PresetDays int    `json:"preset_days"`
	Start      int64  `json:"start"`
	End        int64  `json:"end"`
	Timezone   string `json:"timezone"`
}

type ManagedAccountSnapshotSection struct {
	Observation       *managedinstance.ObservationView `json:"observation,omitempty"`
	LastAttemptAt     int64                            `json:"last_attempt_at"`
	LastAttemptStatus string                           `json:"last_attempt_status"`
	LastErrorCode     string                           `json:"last_error_code,omitempty"`
}

type ManagedAccountSnapshotView struct {
	Range              ManagedAccountRange           `json:"range"`
	Inventory          ManagedAccountSnapshotSection `json:"inventory"`
	AccountOutput      ManagedAccountSnapshotSection `json:"account_output"`
	RefreshRecommended bool                          `json:"refresh_recommended"`
	Task               *model.SystemTaskResponse     `json:"task,omitempty"`
}

type ManagedAccountRefreshView struct {
	Enqueued bool                      `json:"enqueued"`
	Task     *model.SystemTaskResponse `json:"task,omitempty"`
}

type ManagedAccountSyncPayload struct {
	InstanceID int64               `json:"instance_id"`
	ActorID    int                 `json:"actor_id,omitempty"`
	Mode       string              `json:"mode"`
	Range      ManagedAccountRange `json:"range"`
}

type managedAccountSyncHandler struct{}

var (
	managedAccountSlotsOnce sync.Once
	managedAccountSlots     chan struct{}
	managedAccountHostSlots = map[string]*managedInstanceOperationScopedSlot{}
)

func init() {
	RegisterSystemTaskHandler(managedAccountSyncHandler{})
}

func (managedAccountSyncHandler) Type() string {
	return model.SystemTaskTypeManagedAccountSync
}

func NormalizeManagedAccountRange(presetDays int, start int64, end int64, timezone string) (ManagedAccountRange, error) {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = managedAccountDefaultTimezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return ManagedAccountRange{}, managedinstance.ErrInvalidInstance
	}
	if presetDays == 0 && start == 0 && end == 0 {
		presetDays = 7
	}
	if presetDays != 0 {
		valid := false
		for _, days := range managedAccountPresetDays {
			if presetDays == days {
				valid = true
				break
			}
		}
		if !valid {
			return ManagedAccountRange{}, managedinstance.ErrInvalidInstance
		}
		return ManagedAccountRange{
			RangeKey: "preset-" + strconv.Itoa(presetDays), PresetDays: presetDays, Timezone: timezone,
		}, nil
	}
	if start <= 0 || end <= start {
		return ManagedAccountRange{}, managedinstance.ErrInvalidInstance
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", start, end, timezone)))
	return ManagedAccountRange{
		RangeKey: "custom-" + hex.EncodeToString(digest[:12]), Start: start, End: end, Timezone: timezone,
	}, nil
}

func GetManagedAccountSnapshot(instanceID int64, accountRange ManagedAccountRange) (*ManagedAccountSnapshotView, error) {
	if instanceID <= 0 || accountRange.RangeKey == "" {
		return nil, managedinstance.ErrInvalidInstance
	}
	instance, err := managedinstance.Get(instanceID)
	if err != nil {
		return nil, err
	}
	_ = backfillManagedAccountInventory(instanceID)

	inventory, err := findManagedAccountSnapshot(instanceID, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)
	if err != nil {
		return nil, err
	}
	output, err := findManagedAccountSnapshot(instanceID, model.ManagedAccountSnapshotKindOutput, accountRange.RangeKey)
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	touchManagedAccountSnapshots(now, inventory, output)

	mode := managedAccountStandardTaskMode
	if accountRange.PresetDays == 0 {
		mode = managedAccountCustomTaskMode
	}
	task, err := model.GetActiveScopedSystemTask(model.SystemTaskTypeManagedAccountSync, managedAccountTaskScope(instanceID, mode, accountRange.RangeKey))
	if err != nil {
		return nil, err
	}
	view := &ManagedAccountSnapshotView{
		Range:         accountRange,
		Inventory:     managedAccountSnapshotSection(inventory),
		AccountOutput: managedAccountSnapshotSection(output),
	}
	if task != nil {
		response := task.ToResponse()
		view.Task = &response
	}
	outputRequired := instance.Kind != model.ManagedInstanceKindConductor
	view.RefreshRecommended = managedAccountSectionNeedsRefresh(inventory, now)
	if outputRequired {
		view.RefreshRecommended = view.RefreshRecommended || managedAccountSectionNeedsRefresh(output, now)
	}
	return view, nil
}

func EnqueueManagedAccountRefresh(instanceID int64, actorID int, accountRange ManagedAccountRange, force bool) (*ManagedAccountRefreshView, error) {
	if instanceID <= 0 || accountRange.RangeKey == "" {
		return nil, managedinstance.ErrInvalidInstance
	}
	if _, err := managedinstance.Get(instanceID); err != nil {
		return nil, err
	}
	mode := managedAccountStandardTaskMode
	if accountRange.PresetDays == 0 {
		mode = managedAccountCustomTaskMode
	}
	scope := managedAccountTaskScope(instanceID, mode, accountRange.RangeKey)
	if active, err := model.GetActiveScopedSystemTask(model.SystemTaskTypeManagedAccountSync, scope); err != nil {
		return nil, err
	} else if active != nil {
		response := active.ToResponse()
		return &ManagedAccountRefreshView{Task: &response}, nil
	}
	if !force && !managedAccountRefreshDue(instanceID, accountRange) {
		return &ManagedAccountRefreshView{}, nil
	}
	payload := ManagedAccountSyncPayload{InstanceID: instanceID, ActorID: actorID, Mode: mode, Range: accountRange}
	task, created, err := EnqueueScopedSystemTask(model.SystemTaskTypeManagedAccountSync, scope, payload, nil)
	if err != nil {
		return nil, err
	}
	response := task.ToResponse()
	return &ManagedAccountRefreshView{Enqueued: created, Task: &response}, nil
}

func managedAccountRefreshDue(instanceID int64, accountRange ManagedAccountRange) bool {
	now := common.GetTimestamp()
	inventory, _ := findManagedAccountSnapshot(instanceID, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)
	if managedAccountSectionNeedsRefresh(inventory, now) {
		return true
	}
	output, _ := findManagedAccountSnapshot(instanceID, model.ManagedAccountSnapshotKindOutput, accountRange.RangeKey)
	return managedAccountSectionNeedsRefresh(output, now)
}

func managedAccountSectionNeedsRefresh(snapshot *model.ManagedAccountSnapshot, now int64) bool {
	if snapshot == nil || snapshot.LastAttemptAt == 0 {
		return true
	}
	return now-snapshot.LastAttemptAt >= int64(managedAccountRefreshCooldown/time.Second)
}

func managedAccountTaskScope(instanceID int64, mode string, rangeKey string) string {
	if mode == managedAccountStandardTaskMode {
		return strconv.FormatInt(instanceID, 10) + ":standard"
	}
	return strconv.FormatInt(instanceID, 10) + ":" + rangeKey
}

func findManagedAccountSnapshot(instanceID int64, kind string, rangeKey string) (*model.ManagedAccountSnapshot, error) {
	var snapshot model.ManagedAccountSnapshot
	err := model.DB.Where("instance_id = ? AND snapshot_kind = ? AND range_key = ?", instanceID, kind, rangeKey).First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func managedAccountSnapshotSection(snapshot *model.ManagedAccountSnapshot) ManagedAccountSnapshotSection {
	section := ManagedAccountSnapshotSection{}
	if snapshot == nil {
		return section
	}
	section.LastAttemptAt = snapshot.LastAttemptAt
	section.LastAttemptStatus = snapshot.LastAttemptStatus
	section.LastErrorCode = snapshot.LastErrorCode
	if snapshot.ObservedAt <= 0 || snapshot.Payload == "" {
		return section
	}
	var data any
	if json.Unmarshal([]byte(snapshot.Payload), &data) != nil {
		return section
	}
	section.Observation = &managedinstance.ObservationView{
		SourceInstanceID: snapshot.InstanceID,
		ObservedAt:       snapshot.ObservedAt,
		CollectionStatus: model.ManagedInstanceCollectionSucceeded,
		ETag:             snapshot.ETag,
		Data:             data,
	}
	return section
}

func touchManagedAccountSnapshots(now int64, snapshots ...*model.ManagedAccountSnapshot) {
	ids := make([]int64, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot != nil {
			ids = append(ids, snapshot.ID)
		}
	}
	if len(ids) > 0 {
		_ = model.DB.Model(&model.ManagedAccountSnapshot{}).Where("id IN ?", ids).Update("last_accessed_at", now).Error
	}
}

func backfillManagedAccountInventory(instanceID int64) error {
	existing, err := findManagedAccountSnapshot(instanceID, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)
	if err != nil || existing != nil {
		return err
	}
	var legacy model.ManagedInstanceSnapshot
	err = model.DB.Where(
		"instance_id = ? AND snapshot_type = ? AND collection_status = ? AND payload <> ''",
		instanceID, model.ManagedInstanceSnapshotTypeInventory, model.ManagedInstanceCollectionSucceeded,
	).Order("observed_at desc").First(&legacy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	snapshot := &model.ManagedAccountSnapshot{
		InstanceID: instanceID, SnapshotKind: model.ManagedAccountSnapshotKindInventory,
		RangeKey: managedAccountInventoryRangeKey, Timezone: managedAccountDefaultTimezone,
		SchemaVersion: managedAccountSnapshotSchema, ObservedAt: legacy.ObservedAt, ETag: legacy.ETag,
		Payload: legacy.Payload, LastAttemptAt: legacy.ObservedAt,
		LastAttemptStatus: model.ManagedInstanceCollectionSucceeded,
	}
	return model.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(snapshot).Error
}

func saveManagedAccountSnapshot(taskID string, runnerID string, instanceID int64, kind string, accountRange ManagedAccountRange, observation *managedinstance.ObservationView, collectionErr error) error {
	now := common.GetTimestamp()
	status := model.ManagedInstanceCollectionSucceeded
	errorCode := ""
	if collectionErr != nil {
		status = model.ManagedInstanceCollectionFailed
		errorCode = managedAccountErrorCode(collectionErr)
	} else if observation == nil {
		status = model.ManagedInstanceCollectionFailed
		errorCode = "collection_failed"
	} else if observation.CollectionStatus != model.ManagedInstanceCollectionSucceeded {
		status = observation.CollectionStatus
		errorCode = observation.ErrorCode
	}
	rangeKey := accountRange.RangeKey
	if kind == model.ManagedAccountSnapshotKindInventory {
		rangeKey = managedAccountInventoryRangeKey
	}
	insert := &model.ManagedAccountSnapshot{
		InstanceID: instanceID, SnapshotKind: kind, RangeKey: rangeKey,
		PresetDays: accountRange.PresetDays, WindowStart: accountRange.Start, WindowEnd: accountRange.End,
		Timezone: accountRange.Timezone, SchemaVersion: managedAccountSnapshotSchema,
		LastAttemptAt: now, LastAttemptStatus: status, LastErrorCode: errorCode, LastAccessedAt: now,
	}
	updates := map[string]any{
		"preset_days": insert.PresetDays, "window_start": insert.WindowStart, "window_end": insert.WindowEnd,
		"timezone": insert.Timezone, "schema_version": insert.SchemaVersion,
		"last_attempt_at": now, "last_attempt_status": status, "last_error_code": errorCode,
		"updated_at": now,
	}
	if status == model.ManagedInstanceCollectionSucceeded && observation != nil {
		payload, err := json.Marshal(observation.Data)
		if err != nil {
			return err
		}
		insert.ObservedAt = observation.ObservedAt
		insert.ETag = observation.ETag
		insert.Payload = string(payload)
		updates["observed_at"] = insert.ObservedAt
		updates["etag"] = insert.ETag
		updates["payload"] = insert.Payload
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if taskID != "" && runnerID != "" {
			if err := model.RequireValidSystemTaskLease(tx, taskID, runnerID, now); err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "instance_id"}, {Name: "snapshot_kind"}, {Name: "range_key"}},
			DoUpdates: clause.Assignments(updates),
		}).Create(insert).Error
	})
}

func managedAccountErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var probeError *managedinstance.ProbeError
	if errors.As(err, &probeError) && probeError.Code != "" {
		return probeError.Code
	}
	switch {
	case errors.Is(err, managedinstance.ErrInstanceConnectionFailed):
		return "instance_connection_failed"
	case errors.Is(err, managedinstance.ErrRemoteDataUnavailable):
		return "remote_data_unavailable"
	case errors.Is(err, managedinstance.ErrUnsupportedCapability):
		return "unsupported_capability"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "collection_cancelled"
	default:
		return "collection_failed"
	}
}

func resolvedManagedAccountRange(accountRange ManagedAccountRange, now time.Time) (ManagedAccountRange, error) {
	if accountRange.PresetDays == 0 {
		return accountRange, nil
	}
	location, err := time.LoadLocation(accountRange.Timezone)
	if err != nil {
		return ManagedAccountRange{}, err
	}
	localNow := now.In(location)
	start := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	start = start.AddDate(0, 0, -(accountRange.PresetDays - 1))
	accountRange.Start = start.Unix()
	accountRange.End = localNow.Unix()
	return accountRange, nil
}

func collectManagedAccountObservation(ctx context.Context, instanceID int64, actorID int, collect func() (*managedinstance.ObservationView, error)) (*managedinstance.ObservationView, error) {
	if err := managedinstance.EnsureDataConnection(ctx, instanceID, actorID); err != nil {
		return nil, err
	}
	observation, err := collect()
	failed := err != nil || observation == nil || observation.CollectionStatus == model.ManagedInstanceCollectionFailed
	if !failed {
		return observation, nil
	}
	if recoverErr := managedinstance.RecoverDataConnection(ctx, instanceID, actorID); recoverErr != nil {
		if err != nil {
			return observation, err
		}
		return observation, recoverErr
	}
	return collect()
}

func (managedAccountSyncHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := ManagedAccountSyncPayload{}
	if err := task.DecodePayload(&payload); err != nil || payload.InstanceID <= 0 || payload.Range.RangeKey == "" || task.ScopeKey != managedAccountTaskScope(payload.InstanceID, payload.Mode, payload.Range.RangeKey) {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "invalid_account_sync_payload")
		return
	}
	release, err := acquireManagedAccountSlots(ctx, payload.InstanceID)
	if err != nil {
		if ctx.Err() != nil && model.RequeueSystemTask(task.TaskID, runnerID) == nil {
			notifySystemTaskRunner()
			return
		}
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "account_sync_cancelled")
		return
	}
	defer release()

	results := map[string]any{}
	failed := false
	if payload.Mode == managedAccountStandardTaskMode {
		inventory, collectErr := collectManagedAccountObservation(ctx, payload.InstanceID, payload.ActorID, func() (*managedinstance.ObservationView, error) {
			return managedinstance.CollectInventory(ctx, payload.InstanceID, "auto", "")
		})
		if ctx.Err() != nil {
			if model.RequeueSystemTask(task.TaskID, runnerID) == nil {
				notifySystemTaskRunner()
			}
			return
		}
		if err := saveManagedAccountSnapshot(task.TaskID, runnerID, payload.InstanceID, model.ManagedAccountSnapshotKindInventory, payload.Range, inventory, collectErr); err != nil {
			_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, managedAccountErrorCode(err))
			return
		}
		results["inventory"] = managedAccountTaskResult(inventory, collectErr)
		if collectErr != nil || inventory == nil || inventory.CollectionStatus != model.ManagedInstanceCollectionSucceeded {
			failed = true
			for _, days := range managedAccountPresetDays {
				accountRange, _ := NormalizeManagedAccountRange(days, 0, 0, payload.Range.Timezone)
				_ = saveManagedAccountSnapshot(task.TaskID, runnerID, payload.InstanceID, model.ManagedAccountSnapshotKindOutput, accountRange, inventory, collectErr)
			}
		} else {
			failed = collectManagedAccountPresetOutputs(ctx, task, runnerID, payload, results)
		}
	} else {
		failed = collectManagedAccountCustomOutput(ctx, task, runnerID, payload, results)
	}
	if ctx.Err() != nil {
		if model.RequeueSystemTask(task.TaskID, runnerID) == nil {
			notifySystemTaskRunner()
		}
		return
	}
	status := model.SystemTaskStatusSucceeded
	errorCode := ""
	if failed {
		status = model.SystemTaskStatusFailed
		errorCode = "account_collection_failed"
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, status, results, errorCode)
}

func collectManagedAccountPresetOutputs(ctx context.Context, task *model.SystemTask, runnerID string, payload ManagedAccountSyncPayload, results map[string]any) bool {
	instance, err := managedinstance.Get(payload.InstanceID)
	if err != nil {
		return true
	}
	if instance.Kind == model.ManagedInstanceKindConductor {
		return false
	}
	failed := false
	for _, days := range managedAccountPresetDays {
		accountRange, _ := NormalizeManagedAccountRange(days, 0, 0, payload.Range.Timezone)
		accountRange, err = resolvedManagedAccountRange(accountRange, time.Now())
		if err != nil {
			failed = true
			continue
		}
		observation, collectErr := collectManagedAccountObservation(ctx, payload.InstanceID, payload.ActorID, func() (*managedinstance.ObservationView, error) {
			return managedinstance.CollectAccountOutput(ctx, payload.InstanceID, managedinstance.TimeWindow{Start: accountRange.Start, End: accountRange.End})
		})
		if ctx.Err() != nil {
			return true
		}
		if err := saveManagedAccountSnapshot(task.TaskID, runnerID, payload.InstanceID, model.ManagedAccountSnapshotKindOutput, accountRange, observation, collectErr); err != nil {
			return true
		}
		results[accountRange.RangeKey] = managedAccountTaskResult(observation, collectErr)
		if collectErr != nil || observation == nil || observation.CollectionStatus != model.ManagedInstanceCollectionSucceeded {
			failed = true
		}
	}
	return failed
}

func collectManagedAccountCustomOutput(ctx context.Context, task *model.SystemTask, runnerID string, payload ManagedAccountSyncPayload, results map[string]any) bool {
	accountRange := payload.Range
	observation, collectErr := collectManagedAccountObservation(ctx, payload.InstanceID, payload.ActorID, func() (*managedinstance.ObservationView, error) {
		return managedinstance.CollectAccountOutput(ctx, payload.InstanceID, managedinstance.TimeWindow{Start: accountRange.Start, End: accountRange.End})
	})
	if ctx.Err() != nil {
		return true
	}
	if err := saveManagedAccountSnapshot(task.TaskID, runnerID, payload.InstanceID, model.ManagedAccountSnapshotKindOutput, accountRange, observation, collectErr); err != nil {
		return true
	}
	results[accountRange.RangeKey] = managedAccountTaskResult(observation, collectErr)
	return collectErr != nil || observation == nil || observation.CollectionStatus != model.ManagedInstanceCollectionSucceeded
}

func managedAccountTaskResult(observation *managedinstance.ObservationView, collectionErr error) map[string]any {
	status := model.ManagedInstanceCollectionSucceeded
	errorCode := ""
	observedAt := int64(0)
	if collectionErr != nil {
		status = model.ManagedInstanceCollectionFailed
		errorCode = managedAccountErrorCode(collectionErr)
	} else if observation == nil {
		status = model.ManagedInstanceCollectionFailed
		errorCode = "collection_failed"
	} else {
		status = observation.CollectionStatus
		errorCode = observation.ErrorCode
		observedAt = observation.ObservedAt
	}
	return map[string]any{"status": status, "error_code": errorCode, "observed_at": observedAt}
}

func acquireManagedAccountSlots(ctx context.Context, instanceID int64) (func(), error) {
	global := getManagedAccountSlots()
	select {
	case global <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	instance, err := managedinstance.Get(instanceID)
	if err != nil {
		<-global
		return nil, err
	}
	hostKey := strconv.FormatInt(instanceID, 10)
	if parsed, parseErr := url.Parse(instance.BaseURL); parseErr == nil && parsed.Hostname() != "" {
		hostKey = strings.ToLower(parsed.Hostname())
	}
	host := retainManagedInstanceOperationScopedSlot(
		managedAccountHostSlots, hostKey, common.GetEnvOrDefault("MANAGED_ACCOUNT_COLLECTION_MAX_PER_HOST", 1),
	)
	select {
	case host.slots <- struct{}{}:
		return func() {
			releaseManagedInstanceOperationScopedSlot(managedAccountHostSlots, hostKey, host, true)
			<-global
		}, nil
	case <-ctx.Done():
		releaseManagedInstanceOperationScopedSlot(managedAccountHostSlots, hostKey, host, false)
		<-global
		return nil, ctx.Err()
	}
}

func getManagedAccountSlots() chan struct{} {
	managedAccountSlotsOnce.Do(func() {
		limit := boundedManagedInstanceOperationConcurrency(common.GetEnvOrDefault("MANAGED_ACCOUNT_COLLECTION_MAX_CONCURRENCY", 2))
		managedAccountSlots = make(chan struct{}, limit)
	})
	return managedAccountSlots
}

func scheduleDueManagedAccountSyncs(now int64) {
	interval := int64(managedAccountSyncInterval / time.Second)
	forEachManagedInstanceBatch(func(instances []*model.ManagedInstance) bool {
		ids := make([]int64, 0, len(instances))
		for _, instance := range instances {
			if managedAccountKindSupported(instance.Kind) {
				ids = append(ids, instance.Id)
			}
		}
		if len(ids) == 0 {
			return true
		}
		presetKeys := make([]string, 0, len(managedAccountPresetDays))
		for _, days := range managedAccountPresetDays {
			presetKeys = append(presetKeys, "preset-"+strconv.Itoa(days))
		}
		var snapshots []model.ManagedAccountSnapshot
		if err := model.DB.Select("instance_id", "snapshot_kind", "range_key", "last_attempt_at").Where(
			"instance_id IN ? AND ((snapshot_kind = ? AND range_key = ?) OR (snapshot_kind = ? AND range_key IN ?))",
			ids, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey,
			model.ManagedAccountSnapshotKindOutput, presetKeys,
		).Find(&snapshots).Error; err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed account scheduler snapshot query failed: %v", err))
			return false
		}
		latest := make(map[string]int64, len(snapshots))
		for _, snapshot := range snapshots {
			latest[managedAccountScheduleKey(snapshot.InstanceID, snapshot.SnapshotKind, snapshot.RangeKey)] = snapshot.LastAttemptAt
		}
		for _, instance := range instances {
			if !managedAccountKindSupported(instance.Kind) {
				continue
			}
			jitter := instance.Id % 300
			if !managedAccountStandardSyncDue(instance, latest, now, interval+jitter) {
				continue
			}
			accountRange, _ := NormalizeManagedAccountRange(7, 0, 0, managedAccountDefaultTimezone)
			payload := ManagedAccountSyncPayload{InstanceID: instance.Id, Mode: managedAccountStandardTaskMode, Range: accountRange}
			if _, _, err := EnqueueScopedSystemTask(model.SystemTaskTypeManagedAccountSync, managedAccountTaskScope(instance.Id, managedAccountStandardTaskMode, accountRange.RangeKey), payload, nil); err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("managed account scheduler enqueue failed: instance=%d err=%v", instance.Id, err))
			}
		}
		return true
	})
}

func managedAccountStandardSyncDue(instance *model.ManagedInstance, latest map[string]int64, now int64, interval int64) bool {
	if instance == nil {
		return false
	}
	required := []string{managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)}
	if instance.Kind != model.ManagedInstanceKindConductor {
		for _, days := range managedAccountPresetDays {
			required = append(required, managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindOutput, "preset-"+strconv.Itoa(days)))
		}
	}
	for _, key := range required {
		attemptedAt := latest[key]
		if attemptedAt == 0 || now >= attemptedAt+interval {
			return true
		}
	}
	return false
}

func managedAccountScheduleKey(instanceID int64, kind string, rangeKey string) string {
	return strconv.FormatInt(instanceID, 10) + ":" + kind + ":" + rangeKey
}

func cleanupManagedAccountCustomSnapshots(now int64) error {
	cutoff := now - int64(managedAccountCustomRetention/time.Second)
	return model.DB.Where("range_key LIKE ? AND last_accessed_at < ?", "custom-%", cutoff).Delete(&model.ManagedAccountSnapshot{}).Error
}

func managedAccountKindSupported(kind string) bool {
	return kind == model.ManagedInstanceKindNewAPI || kind == model.ManagedInstanceKindHuichuan || kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor
}
