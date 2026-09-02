package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
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
	managedAccountSyncInterval      = 15 * time.Minute
	managedAccountRefreshCooldown   = managedAccountSyncInterval
	managedAccountFailureCooldown   = time.Minute
	managedAccountCustomRetention   = 30 * 24 * time.Hour
	managedAccountDefaultTimezone   = "Asia/Shanghai"
	managedAccountInventoryRangeKey = "inventory"
	managedAccountSnapshotSchema    = 4
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

// ManagedAccountQuerySnapshot is the typed, single-read representation used by
// account queries. It avoids fetching and decoding the same payload again after
// reading snapshot metadata.
type ManagedAccountQuerySnapshot struct {
	InventorySection     ManagedAccountSnapshotSection
	AccountOutputSection ManagedAccountSnapshotSection
	Inventory            *managedinstance.InventoryPage
	AccountOutput        *managedinstance.AccountOutputResult
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
	if accountRange.PresetDays == 0 {
		view.RefreshRecommended = output == nil || output.ObservedAt <= 0
	} else {
		view.RefreshRecommended = managedAccountSectionNeedsRefresh(inventory, now)
		if instance.Kind == model.ManagedInstanceKindClaudeGateway && managedAccountVendorRefreshDue(inventory, now) {
			view.RefreshRecommended = true
		}
		view.RefreshRecommended = view.RefreshRecommended || managedAccountSectionNeedsRefresh(output, now)
	}
	return view, nil
}

func GetManagedAccountQuerySnapshot(instanceID int64, rangeKey string) (*ManagedAccountQuerySnapshot, error) {
	if instanceID <= 0 {
		return nil, managedinstance.ErrInvalidInstance
	}
	if err := backfillManagedAccountInventory(instanceID); err != nil {
		return nil, err
	}
	inventorySnapshot, err := findManagedAccountSnapshot(instanceID, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)
	if err != nil {
		return nil, err
	}
	var outputSnapshot *model.ManagedAccountSnapshot
	if strings.TrimSpace(rangeKey) != "" {
		outputSnapshot, err = findManagedAccountSnapshot(instanceID, model.ManagedAccountSnapshotKindOutput, strings.TrimSpace(rangeKey))
		if err != nil {
			return nil, err
		}
	}
	touchManagedAccountSnapshots(common.GetTimestamp(), inventorySnapshot, outputSnapshot)
	result := &ManagedAccountQuerySnapshot{
		InventorySection:     managedAccountSnapshotSectionMetadata(inventorySnapshot),
		AccountOutputSection: managedAccountSnapshotSectionMetadata(outputSnapshot),
	}
	if inventorySnapshot != nil && inventorySnapshot.ObservedAt > 0 && strings.TrimSpace(inventorySnapshot.Payload) != "" {
		var inventory managedinstance.InventoryPage
		if err := json.Unmarshal([]byte(inventorySnapshot.Payload), &inventory); err != nil {
			return nil, err
		}
		for index := range inventory.Items {
			if inventory.Items[index].IDText == "" {
				inventory.Items[index].IDText = strconv.FormatInt(inventory.Items[index].ID, 10)
			}
		}
		result.Inventory = &inventory
	}
	if outputSnapshot != nil && outputSnapshot.ObservedAt > 0 && strings.TrimSpace(outputSnapshot.Payload) != "" {
		var output managedinstance.AccountOutputResult
		if err := json.Unmarshal([]byte(outputSnapshot.Payload), &output); err != nil {
			return nil, err
		}
		for index := range output.Items {
			if output.Items[index].Account.IDText == "" {
				output.Items[index].Account.IDText = strconv.FormatInt(output.Items[index].Account.ID, 10)
			}
		}
		result.AccountOutput = &output
	}
	return result, nil
}

// GetManagedAccountInventorySnapshot returns the last successful inventory used by
// account management. Export creation uses this snapshot to freeze selected rows.
func GetManagedAccountInventorySnapshot(instanceID int64) (*managedinstance.InventoryPage, error) {
	if instanceID <= 0 {
		return nil, managedinstance.ErrInvalidInstance
	}
	if err := backfillManagedAccountInventory(instanceID); err != nil {
		return nil, err
	}
	snapshot, err := findManagedAccountSnapshot(instanceID, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)
	if err != nil || snapshot == nil || snapshot.ObservedAt <= 0 || strings.TrimSpace(snapshot.Payload) == "" {
		return nil, err
	}
	var page managedinstance.InventoryPage
	if err := json.Unmarshal([]byte(snapshot.Payload), &page); err != nil {
		return nil, err
	}
	for index := range page.Items {
		if page.Items[index].IDText == "" {
			page.Items[index].IDText = strconv.FormatInt(page.Items[index].ID, 10)
		}
	}
	return &page, nil
}

// GetManagedAccountOutputSnapshot returns the account-output rows for the
// selected range. Older clients may omit rangeKey, so fall back to the latest
// successful output snapshot in that case.
func GetManagedAccountOutputSnapshot(instanceID int64, rangeKey string) (*managedinstance.AccountOutputResult, error) {
	if instanceID <= 0 {
		return nil, managedinstance.ErrInvalidInstance
	}
	var snapshot *model.ManagedAccountSnapshot
	var err error
	if strings.TrimSpace(rangeKey) != "" {
		snapshot, err = findManagedAccountSnapshot(instanceID, model.ManagedAccountSnapshotKindOutput, strings.TrimSpace(rangeKey))
	} else {
		var latest model.ManagedAccountSnapshot
		err = model.DB.Where(
			"instance_id = ? AND snapshot_kind = ? AND observed_at > 0 AND payload <> ''",
			instanceID, model.ManagedAccountSnapshotKindOutput,
		).Order("observed_at desc").First(&latest).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err == nil {
			snapshot = &latest
		}
	}
	if err != nil || snapshot == nil || snapshot.ObservedAt <= 0 || strings.TrimSpace(snapshot.Payload) == "" {
		return nil, err
	}
	var result managedinstance.AccountOutputResult
	if err := json.Unmarshal([]byte(snapshot.Payload), &result); err != nil {
		return nil, err
	}
	for index := range result.Items {
		if result.Items[index].Account.IDText == "" {
			result.Items[index].Account.IDText = strconv.FormatInt(result.Items[index].Account.ID, 10)
		}
	}
	return &result, nil
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
	cooldown := managedAccountRefreshCooldown
	if snapshot.LastAttemptStatus == model.ManagedInstanceCollectionFailed {
		cooldown = managedAccountFailureCooldown
	}
	return now-snapshot.LastAttemptAt >= int64(cooldown/time.Second)
}

func managedAccountVendorRefreshDue(snapshot *model.ManagedAccountSnapshot, now int64) bool {
	if managedAccountVendorMetadataAvailable(snapshot) {
		return false
	}
	if snapshot == nil || snapshot.LastAttemptAt == 0 {
		return true
	}
	if snapshot.LastAttemptStatus == model.ManagedInstanceCollectionFailed {
		return now-snapshot.LastAttemptAt >= int64(managedAccountFailureCooldown/time.Second)
	}
	return true
}

func managedAccountVendorMetadataAvailable(snapshot *model.ManagedAccountSnapshot) bool {
	if snapshot == nil || strings.TrimSpace(snapshot.Payload) == "" {
		return false
	}
	var metadata struct {
		VendorCollectionStatus string `json:"vendor_collection_status"`
	}
	return json.Unmarshal([]byte(snapshot.Payload), &metadata) == nil && strings.TrimSpace(metadata.VendorCollectionStatus) != ""
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

func managedAccountSnapshotSectionMetadata(snapshot *model.ManagedAccountSnapshot) ManagedAccountSnapshotSection {
	section := ManagedAccountSnapshotSection{}
	if snapshot == nil {
		return section
	}
	section.LastAttemptAt = snapshot.LastAttemptAt
	section.LastAttemptStatus = snapshot.LastAttemptStatus
	section.LastErrorCode = snapshot.LastErrorCode
	if snapshot.ObservedAt > 0 && strings.TrimSpace(snapshot.Payload) != "" {
		section.Observation = &managedinstance.ObservationView{
			SourceInstanceID: snapshot.InstanceID,
			ObservedAt:       snapshot.ObservedAt,
			CollectionStatus: model.ManagedInstanceCollectionSucceeded,
			ETag:             snapshot.ETag,
		}
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
		cutoff := now - int64(time.Hour/time.Second)
		if err := model.DB.Model(&model.ManagedAccountSnapshot{}).
			Where("id IN ? AND (last_accessed_at = 0 OR last_accessed_at < ?)", ids, cutoff).
			Update("last_accessed_at", now).Error; err != nil {
			logger.LogWarn(context.Background(), "managed account snapshot access timestamp update failed: "+err.Error())
		}
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
	schemaVersion := legacy.SchemaVersion
	if schemaVersion <= 0 {
		schemaVersion = 1
	}
	snapshot := &model.ManagedAccountSnapshot{
		InstanceID: instanceID, SnapshotKind: model.ManagedAccountSnapshotKindInventory,
		RangeKey: managedAccountInventoryRangeKey, Timezone: managedAccountDefaultTimezone,
		SchemaVersion: schemaVersion, ObservedAt: legacy.ObservedAt, ETag: legacy.ETag,
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
		"timezone":        insert.Timezone,
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
		updates["schema_version"] = insert.SchemaVersion
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		if taskID != "" && runnerID != "" {
			if err := model.RequireValidSystemTaskLease(tx, taskID, runnerID, now); err != nil {
				return err
			}
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "instance_id"}, {Name: "snapshot_kind"}, {Name: "range_key"}},
			DoUpdates: clause.Assignments(updates),
		}).Create(insert).Error; err != nil {
			return err
		}
		if status == model.ManagedInstanceCollectionSucceeded && observation != nil {
			return archiveManagedAccountDailySnapshot(tx, insert, now)
		}
		return nil
	})
}

func archiveManagedAccountDailySnapshot(tx *gorm.DB, snapshot *model.ManagedAccountSnapshot, capturedAt int64) error {
	if tx == nil || snapshot == nil || !managedAccountDailyArchiveEligible(snapshot.SnapshotKind, snapshot.RangeKey) || snapshot.ObservedAt <= 0 || snapshot.Payload == "" {
		return nil
	}
	snapshotDate, boundaryAt := managedAccountArchiveDay(snapshot.ObservedAt)
	archive := &model.ManagedAccountDailySnapshot{
		InstanceID: snapshot.InstanceID, SnapshotKind: snapshot.SnapshotKind, RangeKey: snapshot.RangeKey,
		SnapshotDate: snapshotDate, BoundaryAt: boundaryAt,
		PresetDays: snapshot.PresetDays, WindowStart: snapshot.WindowStart, WindowEnd: snapshot.WindowEnd,
		Timezone: snapshot.Timezone, SchemaVersion: snapshot.SchemaVersion,
		ObservedAt: snapshot.ObservedAt, CapturedAt: capturedAt, ETag: snapshot.ETag, Payload: snapshot.Payload,
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "instance_id"}, {Name: "snapshot_kind"}, {Name: "range_key"}, {Name: "snapshot_date"}},
		DoNothing: true,
	}).Create(archive).Error
}

func managedAccountDailyArchiveEligible(kind string, rangeKey string) bool {
	if kind == model.ManagedAccountSnapshotKindInventory {
		return rangeKey == managedAccountInventoryRangeKey
	}
	if kind != model.ManagedAccountSnapshotKindOutput {
		return false
	}
	for _, days := range managedAccountPresetDays {
		if rangeKey == "preset-"+strconv.Itoa(days) {
			return true
		}
	}
	return false
}

func managedAccountArchiveDay(timestamp int64) (string, int64) {
	location := time.FixedZone(managedAccountDefaultTimezone, 8*60*60)
	observed := time.Unix(timestamp, 0).In(location)
	boundary := time.Date(observed.Year(), observed.Month(), observed.Day(), 0, 0, 0, 0, location)
	return boundary.Format("2006-01-02"), boundary.Unix()
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
	defer publishManagedAccountSnapshotEvent(payload.InstanceID)

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
	failed := false
	for _, days := range managedAccountPresetDays {
		accountRange, _ := NormalizeManagedAccountRange(days, 0, 0, payload.Range.Timezone)
		accountRange, err := resolvedManagedAccountRange(accountRange, time.Now())
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
	archiveDate, archiveBoundary := managedAccountArchiveDay(now)
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
		if err := model.DB.Select("instance_id", "snapshot_kind", "range_key", "schema_version", "last_attempt_at", "last_attempt_status").Where(
			"instance_id IN ? AND ((snapshot_kind = ? AND range_key = ?) OR (snapshot_kind = ? AND range_key IN ?))",
			ids, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey,
			model.ManagedAccountSnapshotKindOutput, presetKeys,
		).Find(&snapshots).Error; err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed account scheduler snapshot query failed: %v", err))
			return false
		}
		latest := make(map[string]managedAccountScheduleState, len(snapshots))
		for _, snapshot := range snapshots {
			latest[managedAccountScheduleKey(snapshot.InstanceID, snapshot.SnapshotKind, snapshot.RangeKey)] = managedAccountScheduleState{
				AttemptedAt:             snapshot.LastAttemptAt,
				Status:                  snapshot.LastAttemptStatus,
				VendorMetadataAvailable: snapshot.SnapshotKind != model.ManagedAccountSnapshotKindInventory || snapshot.SchemaVersion >= managedAccountSnapshotSchema,
			}
		}
		var archives []model.ManagedAccountDailySnapshot
		if err := model.DB.Select("instance_id", "snapshot_kind", "range_key").Where(
			"instance_id IN ? AND snapshot_date = ?", ids, archiveDate,
		).Find(&archives).Error; err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed account scheduler daily archive query failed: %v", err))
			return false
		}
		dailyArchived := make(map[string]bool, len(archives))
		for _, archive := range archives {
			dailyArchived[managedAccountScheduleKey(archive.InstanceID, archive.SnapshotKind, archive.RangeKey)] = true
		}
		for _, instance := range instances {
			if !managedAccountKindSupported(instance.Kind) {
				continue
			}
			if !managedAccountDailyArchiveSyncDue(instance, latest, dailyArchived, archiveBoundary, now) && !managedAccountStandardSyncDue(instance, latest, now) {
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

func managedAccountDailyArchiveSyncDue(instance *model.ManagedInstance, latest map[string]managedAccountScheduleState, dailyArchived map[string]bool, boundaryAt int64, now int64) bool {
	if instance == nil {
		return false
	}
	required := []string{managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)}
	for _, days := range managedAccountPresetDays {
		required = append(required, managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindOutput, "preset-"+strconv.Itoa(days)))
	}
	for _, key := range required {
		if dailyArchived[key] {
			continue
		}
		state := latest[key]
		if state.AttemptedAt == 0 || state.AttemptedAt < boundaryAt {
			return true
		}
		cooldown := managedAccountSyncInterval
		if state.Status == model.ManagedInstanceCollectionFailed {
			cooldown = managedAccountFailureCooldown
		}
		if now >= state.AttemptedAt+int64(cooldown/time.Second) {
			return true
		}
	}
	return false
}

type managedAccountScheduleState struct {
	AttemptedAt             int64
	Status                  string
	VendorMetadataAvailable bool
}

func managedAccountStandardSyncDue(instance *model.ManagedInstance, latest map[string]managedAccountScheduleState, now int64) bool {
	if instance == nil {
		return false
	}
	inventoryKey := managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey)
	if instance.Kind == model.ManagedInstanceKindClaudeGateway {
		inventory := latest[inventoryKey]
		if !inventory.VendorMetadataAvailable {
			if inventory.Status != model.ManagedInstanceCollectionFailed {
				return true
			}
			return inventory.AttemptedAt == 0 || now >= inventory.AttemptedAt+int64(managedAccountFailureCooldown/time.Second)
		}
	}
	required := []string{inventoryKey}
	for _, days := range managedAccountPresetDays {
		required = append(required, managedAccountScheduleKey(instance.Id, model.ManagedAccountSnapshotKindOutput, "preset-"+strconv.Itoa(days)))
	}
	for _, key := range required {
		state := latest[key]
		if state.AttemptedAt == 0 {
			return true
		}
		cooldown := managedAccountSyncInterval
		if state.Status == model.ManagedInstanceCollectionFailed {
			cooldown = managedAccountFailureCooldown
		}
		if now >= state.AttemptedAt+int64(cooldown/time.Second) {
			return true
		}
	}
	return false
}

func currentManagedAccountSnapshotEvent(instanceID int64) (*ManagedAccountSnapshotEvent, error) {
	if instanceID <= 0 {
		return nil, managedinstance.ErrInvalidInstance
	}
	presetKeys := make([]string, 0, len(managedAccountPresetDays))
	for _, days := range managedAccountPresetDays {
		presetKeys = append(presetKeys, "preset-"+strconv.Itoa(days))
	}
	var snapshots []model.ManagedAccountSnapshot
	if err := model.DB.Select(
		"instance_id", "snapshot_kind", "range_key", "observed_at", "last_attempt_at", "last_attempt_status", "last_error_code",
	).Where(
		"instance_id = ? AND ((snapshot_kind = ? AND range_key = ?) OR (snapshot_kind = ? AND range_key IN ?))",
		instanceID, model.ManagedAccountSnapshotKindInventory, managedAccountInventoryRangeKey,
		model.ManagedAccountSnapshotKindOutput, presetKeys,
	).Find(&snapshots).Error; err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return nil, nil
	}
	event := &ManagedAccountSnapshotEvent{InstanceID: instanceID, RangeKeys: make([]string, 0, len(snapshots))}
	failedAt := int64(0)
	failedErrorCode := ""
	for _, snapshot := range snapshots {
		event.RangeKeys = append(event.RangeKeys, snapshot.RangeKey)
		if snapshot.ObservedAt > event.ObservedAt {
			event.ObservedAt = snapshot.ObservedAt
		}
		if snapshot.LastAttemptAt > event.LastAttemptAt {
			event.LastAttemptAt = snapshot.LastAttemptAt
			event.LastAttemptStatus = snapshot.LastAttemptStatus
			event.LastErrorCode = snapshot.LastErrorCode
		}
		if snapshot.LastAttemptStatus == model.ManagedInstanceCollectionFailed && snapshot.LastAttemptAt >= failedAt {
			failedAt = snapshot.LastAttemptAt
			failedErrorCode = snapshot.LastErrorCode
		}
	}
	if failedAt > 0 {
		event.LastAttemptStatus = model.ManagedInstanceCollectionFailed
		event.LastErrorCode = failedErrorCode
	}
	sort.Strings(event.RangeKeys)
	return event, nil
}

func publishManagedAccountSnapshotEvent(instanceID int64) {
	event, err := currentManagedAccountSnapshotEvent(instanceID)
	if err != nil || event == nil {
		return
	}
	broadcastManagedRealtimeEvent(ManagedRealtimeEvent{Type: "account_snapshot", AccountSnapshot: event})
}

func managedAccountScheduleKey(instanceID int64, kind string, rangeKey string) string {
	return strconv.FormatInt(instanceID, 10) + ":" + kind + ":" + rangeKey
}

func cleanupManagedAccountCustomSnapshots(now int64) error {
	cutoff := now - int64(managedAccountCustomRetention/time.Second)
	return model.DB.Where("range_key LIKE ? AND last_accessed_at < ?", "custom-%", cutoff).Delete(&model.ManagedAccountSnapshot{}).Error
}

func managedAccountKindSupported(kind string) bool {
	return kind == model.ManagedInstanceKindNewAPI || kind == model.ManagedInstanceKindHuichuan || kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor || kind == model.ManagedInstanceKindClaudeGateway
}
