package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

const (
	managedDashboardTimezone        = "Asia/Shanghai"
	managedDashboardRefreshInterval = time.Minute
	managedDashboardRetryInterval   = 15 * time.Second
	managedDashboardCustomRetention = 30 * 24 * time.Hour
)

var managedDashboardPresetDays = [...]int{1, 7, 14, 30}

var errManagedDashboardBusy = fmt.Errorf("managed dashboard collection is already running")

type ManagedDashboardRange struct {
	RangeKey   string `json:"range_key"`
	PresetDays int    `json:"preset_days"`
	Start      int64  `json:"start"`
	End        int64  `json:"end"`
	Timezone   string `json:"timezone"`
}

type ManagedDashboardSnapshotSection struct {
	Range             ManagedDashboardRange            `json:"range"`
	Observation       *managedinstance.ObservationView `json:"observation,omitempty"`
	LastAttemptAt     int64                            `json:"last_attempt_at"`
	LastAttemptStatus string                           `json:"last_attempt_status"`
	LastErrorCode     string                           `json:"last_error_code,omitempty"`
	Stale             bool                             `json:"stale"`
}

type ManagedDashboardInstanceSnapshotView struct {
	InstanceID int64                           `json:"instance_id"`
	Summary    ManagedDashboardSnapshotSection `json:"summary"`
	Today      ManagedDashboardSnapshotSection `json:"today"`
}

type ManagedDashboardSnapshotListView struct {
	Range ManagedDashboardRange                  `json:"range"`
	Items []ManagedDashboardInstanceSnapshotView `json:"items"`
}

type ManagedDashboardRefreshPayload struct {
	InstanceID int64                 `json:"instance_id"`
	ActorID    int                   `json:"actor_id,omitempty"`
	Range      ManagedDashboardRange `json:"range"`
}

type ManagedDashboardRefreshView struct {
	InstanceID int64                     `json:"instance_id"`
	Enqueued   bool                      `json:"enqueued"`
	Task       *model.SystemTaskResponse `json:"task,omitempty"`
}

type ManagedDashboardEvent struct {
	Type        string                           `json:"type"`
	InstanceID  int64                            `json:"instance_id,omitempty"`
	Snapshot    *ManagedDashboardSnapshotSection `json:"snapshot,omitempty"`
	InstanceIDs []int64                          `json:"instance_ids,omitempty"`
}

type managedDashboardRefreshHandler struct{}

type managedDashboardSubscriber struct {
	ids    map[int64]struct{}
	events chan ManagedDashboardEvent
}

var (
	managedDashboardCollectorOnce sync.Once
	managedDashboardSlotsOnce     sync.Once
	managedDashboardSlots         chan struct{}
	managedDashboardHostSlots     = map[string]*managedInstanceOperationScopedSlot{}
	managedDashboardInFlight      sync.Map
	managedDashboardRetryMu       sync.Mutex
	managedDashboardRetries       = map[int64]*time.Timer{}
	managedDashboardSubscribersMu sync.RWMutex
	managedDashboardSubscribers   = map[*managedDashboardSubscriber]struct{}{}
	managedDashboardTopologyMu    sync.Mutex
	managedDashboardTopologyKey   string
)

func init() {
	RegisterSystemTaskHandler(managedDashboardRefreshHandler{})
}

func (managedDashboardRefreshHandler) Type() string {
	return model.SystemTaskTypeManagedDashboardRefresh
}

func NormalizeManagedDashboardRange(presetDays int, start, end int64) (ManagedDashboardRange, error) {
	if presetDays == 0 && start == 0 && end == 0 {
		presetDays = 7
	}
	if presetDays != 0 {
		valid := false
		for _, days := range managedDashboardPresetDays {
			if days == presetDays {
				valid = true
				break
			}
		}
		if !valid {
			return ManagedDashboardRange{}, managedinstance.ErrInvalidInstance
		}
		result := ManagedDashboardRange{RangeKey: "preset-" + strconv.Itoa(presetDays), PresetDays: presetDays, Timezone: managedDashboardTimezone}
		return resolveManagedDashboardRange(result, time.Now())
	}
	if start <= 0 || end <= start {
		return ManagedDashboardRange{}, managedinstance.ErrInvalidInstance
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", start, end, managedDashboardTimezone)))
	return ManagedDashboardRange{
		RangeKey: "custom-" + hex.EncodeToString(digest[:12]), Start: start, End: end, Timezone: managedDashboardTimezone,
	}, nil
}

func resolveManagedDashboardRange(input ManagedDashboardRange, now time.Time) (ManagedDashboardRange, error) {
	if input.PresetDays == 0 {
		return input, nil
	}
	location, err := time.LoadLocation(managedDashboardTimezone)
	if err != nil {
		return ManagedDashboardRange{}, err
	}
	localNow := now.In(location)
	dayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
	input.Start = dayStart.AddDate(0, 0, -(input.PresetDays - 1)).Unix()
	input.End = dayStart.AddDate(0, 0, 1).Add(-time.Second).Unix()
	input.Timezone = managedDashboardTimezone
	return input, nil
}

func GetManagedDashboardSnapshots(instanceIDs []int64, dashboardRange ManagedDashboardRange) (*ManagedDashboardSnapshotListView, error) {
	items := make([]ManagedDashboardInstanceSnapshotView, 0, len(instanceIDs))
	todayRange, _ := NormalizeManagedDashboardRange(1, 0, 0)
	for _, instanceID := range instanceIDs {
		if _, err := managedinstance.Get(instanceID); err != nil {
			return nil, err
		}
		summary, err := loadManagedDashboardSection(instanceID, dashboardRange)
		if err != nil {
			return nil, err
		}
		today := summary
		if dashboardRange.RangeKey != todayRange.RangeKey {
			today, err = loadManagedDashboardSection(instanceID, todayRange)
			if err != nil {
				return nil, err
			}
		}
		items = append(items, ManagedDashboardInstanceSnapshotView{InstanceID: instanceID, Summary: summary, Today: today})
	}
	return &ManagedDashboardSnapshotListView{Range: dashboardRange, Items: items}, nil
}

func loadManagedDashboardSection(instanceID int64, dashboardRange ManagedDashboardRange) (ManagedDashboardSnapshotSection, error) {
	section := ManagedDashboardSnapshotSection{Range: dashboardRange}
	var snapshot model.ManagedDashboardSnapshot
	err := model.DB.Where("instance_id = ? AND range_key = ?", instanceID, dashboardRange.RangeKey).First(&snapshot).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return section, nil
		}
		return section, err
	}
	section.Range.Start = snapshot.WindowStart
	section.Range.End = snapshot.WindowEnd
	section.LastAttemptAt = snapshot.LastAttemptAt
	section.LastAttemptStatus = snapshot.LastAttemptStatus
	section.LastErrorCode = snapshot.LastErrorCode
	section.Stale = snapshot.LastAttemptStatus == model.ManagedInstanceCollectionFailed || snapshot.ObservedAt == 0 || common.GetTimestamp()-snapshot.ObservedAt > 120
	if snapshot.ObservedAt > 0 && strings.TrimSpace(snapshot.Payload) != "" {
		var summary managedinstance.SummaryResult
		if json.Unmarshal([]byte(snapshot.Payload), &summary) == nil {
			section.Observation = &managedinstance.ObservationView{
				SourceInstanceID: instanceID, ObservedAt: snapshot.ObservedAt,
				CollectionStatus: model.ManagedInstanceCollectionSucceeded,
				ETag:             snapshot.ETag, Data: &summary,
			}
		}
	}
	_ = model.DB.Model(&model.ManagedDashboardSnapshot{}).Where("id = ?", snapshot.ID).Update("last_accessed_at", common.GetTimestamp()).Error
	return section, nil
}

func EnqueueManagedDashboardRefresh(instanceID int64, actorID int, dashboardRange ManagedDashboardRange) (*ManagedDashboardRefreshView, error) {
	if instanceID <= 0 || dashboardRange.RangeKey == "" {
		return nil, managedinstance.ErrInvalidInstance
	}
	if _, err := managedinstance.Get(instanceID); err != nil {
		return nil, err
	}
	payload := ManagedDashboardRefreshPayload{InstanceID: instanceID, ActorID: actorID, Range: dashboardRange}
	task, created, err := EnqueueScopedSystemTask(model.SystemTaskTypeManagedDashboardRefresh, managedDashboardTaskScope(instanceID, dashboardRange.RangeKey), payload, nil)
	if err != nil {
		return nil, err
	}
	response := task.ToResponse()
	return &ManagedDashboardRefreshView{InstanceID: instanceID, Enqueued: created, Task: &response}, nil
}

func managedDashboardTaskScope(instanceID int64, rangeKey string) string {
	return strconv.FormatInt(instanceID, 10) + ":" + rangeKey
}

func (managedDashboardRefreshHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := ManagedDashboardRefreshPayload{}
	if err := task.DecodePayload(&payload); err != nil || payload.InstanceID <= 0 || payload.Range.RangeKey == "" || task.ScopeKey != managedDashboardTaskScope(payload.InstanceID, payload.Range.RangeKey) {
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, "invalid_dashboard_refresh_payload")
		return
	}
	err := refreshManagedDashboardRange(ctx, payload.InstanceID, payload.ActorID, payload.Range)
	if ctx.Err() != nil {
		if model.RequeueSystemTask(task.TaskID, runnerID) == nil {
			notifySystemTaskRunner()
		}
		return
	}
	if err != nil {
		if err == errManagedDashboardBusy {
			if model.RequeueSystemTask(task.TaskID, runnerID) == nil {
				notifySystemTaskRunner()
			}
			return
		}
		_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, nil, managedAccountErrorCode(err))
		return
	}
	_ = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, map[string]any{"range_key": payload.Range.RangeKey}, "")
}

func StartManagedDashboardCollector() {
	managedDashboardCollectorOnce.Do(func() {
		if !common.IsMasterNode || model.DB == nil {
			return
		}
		go func() {
			collectManagedDashboardPresets()
			ticker := time.NewTicker(managedDashboardRefreshInterval)
			cleanupTicker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			defer cleanupTicker.Stop()
			for {
				select {
				case <-ticker.C:
					collectManagedDashboardPresets()
				case <-cleanupTicker.C:
					cutoff := common.GetTimestamp() - int64(managedDashboardCustomRetention/time.Second)
					_ = model.DB.Where("preset_days = 0 AND last_accessed_at < ?", cutoff).Delete(&model.ManagedDashboardSnapshot{}).Error
				}
			}
		}()
	})
}

func collectManagedDashboardPresets() {
	var instances []model.ManagedInstance
	if err := model.DB.Order("id ASC").Find(&instances).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("dashboard collector instance query failed: %v", err))
		return
	}
	ids := make([]int64, 0, len(instances))
	for index := range instances {
		instance := instances[index]
		if instance.Kind == model.ManagedInstanceKindGeneric {
			continue
		}
		ids = append(ids, instance.Id)
		go func() {
			if err := refreshManagedDashboardPresets(context.Background(), instance.Id, 0); err != nil {
				if err == errManagedDashboardBusy {
					return
				}
				logger.LogWarn(context.Background(), fmt.Sprintf("dashboard collection failed: instance=%d err=%v", instance.Id, err))
				scheduleManagedDashboardRetry(instance.Id)
			}
		}()
	}
	publishManagedDashboardTopology(instances, ids)
}

func publishManagedDashboardTopology(instances []model.ManagedInstance, ids []int64) {
	parts := make([]string, 0, len(instances))
	for index := range instances {
		instance := instances[index]
		if instance.Kind == model.ManagedInstanceKindGeneric {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d:%s:%d", instance.Id, instance.Status, instance.UpdatedAt))
	}
	key := strings.Join(parts, ",")
	managedDashboardTopologyMu.Lock()
	if key == managedDashboardTopologyKey {
		managedDashboardTopologyMu.Unlock()
		return
	}
	managedDashboardTopologyKey = key
	managedDashboardTopologyMu.Unlock()
	publishManagedDashboardEvent(ManagedDashboardEvent{Type: "topology", InstanceIDs: ids})
}

func scheduleManagedDashboardRetry(instanceID int64) {
	managedDashboardRetryMu.Lock()
	defer managedDashboardRetryMu.Unlock()
	if _, exists := managedDashboardRetries[instanceID]; exists {
		return
	}
	managedDashboardRetries[instanceID] = time.AfterFunc(managedDashboardRetryInterval, func() {
		managedDashboardRetryMu.Lock()
		delete(managedDashboardRetries, instanceID)
		managedDashboardRetryMu.Unlock()
		if err := refreshManagedDashboardPresets(context.Background(), instanceID, 0); err != nil {
			scheduleManagedDashboardRetry(instanceID)
		}
	})
}

func refreshManagedDashboardRange(ctx context.Context, instanceID int64, actorID int, dashboardRange ManagedDashboardRange) error {
	if dashboardRange.PresetDays != 0 {
		return refreshManagedDashboardPresets(ctx, instanceID, actorID)
	}
	release, acquired, err := acquireManagedDashboardSlots(ctx, instanceID)
	if err != nil {
		return err
	}
	if !acquired {
		return errManagedDashboardBusy
	}
	defer release()
	return collectAndSaveManagedDashboardRange(ctx, instanceID, actorID, dashboardRange)
}

func refreshManagedDashboardPresets(ctx context.Context, instanceID int64, actorID int) error {
	release, acquired, err := acquireManagedDashboardSlots(ctx, instanceID)
	if err != nil {
		return err
	}
	if !acquired {
		return errManagedDashboardBusy
	}
	defer release()
	for _, days := range managedDashboardPresetDays {
		dashboardRange, _ := NormalizeManagedDashboardRange(days, 0, 0)
		if err := saveManagedDashboardRunning(instanceID, dashboardRange); err != nil {
			return err
		}
	}
	thirty, _ := NormalizeManagedDashboardRange(30, 0, 0)
	if err := managedinstance.EnsureDataConnection(ctx, instanceID, actorID); err != nil {
		markManagedDashboardPresetFailures(instanceID, err)
		return err
	}
	instance, err := managedinstance.Get(instanceID)
	if err != nil {
		markManagedDashboardPresetFailures(instanceID, err)
		return err
	}
	if instance.Kind == model.ManagedInstanceKindClaudeGateway {
		return refreshClaudeGatewayDashboardPresets(ctx, instanceID, actorID)
	}
	summary, err := managedinstance.CollectSummaryData(ctx, instanceID, managedinstance.TimeWindow{Start: thirty.Start, End: thirty.End, Timezone: managedDashboardTimezone})
	if err != nil {
		if managedinstance.RecoverDataConnection(ctx, instanceID, actorID) == nil {
			summary, err = managedinstance.CollectSummaryData(ctx, instanceID, managedinstance.TimeWindow{Start: thirty.Start, End: thirty.End, Timezone: managedDashboardTimezone})
		}
	}
	if err != nil || summary == nil {
		if err == nil {
			err = managedinstance.ErrRemoteDataUnavailable
		}
		markManagedDashboardPresetFailures(instanceID, err)
		return err
	}
	observedAt := common.GetTimestamp()
	for _, days := range managedDashboardPresetDays {
		dashboardRange, _ := NormalizeManagedDashboardRange(days, 0, 0)
		derived := deriveManagedDashboardSummary(summary, dashboardRange)
		if err := saveManagedDashboardSuccess(instanceID, dashboardRange, observedAt, derived); err != nil {
			return err
		}
	}
	cancelManagedDashboardRetry(instanceID)
	return nil
}

func refreshClaudeGatewayDashboardPresets(ctx context.Context, instanceID int64, actorID int) error {
	observedAt := common.GetTimestamp()
	var firstErr error
	recovered := false
	for _, days := range managedDashboardPresetDays {
		dashboardRange, _ := NormalizeManagedDashboardRange(days, 0, 0)
		window := managedinstance.TimeWindow{Start: dashboardRange.Start, End: dashboardRange.End, Timezone: managedDashboardTimezone}
		summary, err := managedinstance.CollectSummaryData(ctx, instanceID, window)
		if err != nil && !recovered {
			recovered = true
			if managedinstance.RecoverDataConnection(ctx, instanceID, actorID) == nil {
				summary, err = managedinstance.CollectSummaryData(ctx, instanceID, window)
			}
		}
		if err != nil || summary == nil {
			if err == nil {
				err = managedinstance.ErrRemoteDataUnavailable
			}
			_ = saveManagedDashboardFailure(instanceID, dashboardRange, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := saveManagedDashboardSuccess(instanceID, dashboardRange, observedAt, summary); err != nil {
			return err
		}
	}
	if firstErr != nil {
		return firstErr
	}
	cancelManagedDashboardRetry(instanceID)
	return nil
}

func cancelManagedDashboardRetry(instanceID int64) {
	managedDashboardRetryMu.Lock()
	if timer := managedDashboardRetries[instanceID]; timer != nil {
		timer.Stop()
		delete(managedDashboardRetries, instanceID)
	}
	managedDashboardRetryMu.Unlock()
}

func collectAndSaveManagedDashboardRange(ctx context.Context, instanceID int64, actorID int, dashboardRange ManagedDashboardRange) error {
	if err := saveManagedDashboardRunning(instanceID, dashboardRange); err != nil {
		return err
	}
	if err := managedinstance.EnsureDataConnection(ctx, instanceID, actorID); err != nil {
		_ = saveManagedDashboardFailure(instanceID, dashboardRange, err)
		return err
	}
	summary, err := managedinstance.CollectSummaryData(ctx, instanceID, managedinstance.TimeWindow{Start: dashboardRange.Start, End: dashboardRange.End, Timezone: managedDashboardTimezone})
	if err != nil {
		if managedinstance.RecoverDataConnection(ctx, instanceID, actorID) == nil {
			summary, err = managedinstance.CollectSummaryData(ctx, instanceID, managedinstance.TimeWindow{Start: dashboardRange.Start, End: dashboardRange.End, Timezone: managedDashboardTimezone})
		}
	}
	if err != nil || summary == nil {
		if err == nil {
			err = managedinstance.ErrRemoteDataUnavailable
		}
		_ = saveManagedDashboardFailure(instanceID, dashboardRange, err)
		return err
	}
	return saveManagedDashboardSuccess(instanceID, dashboardRange, common.GetTimestamp(), summary)
}

func deriveManagedDashboardSummary(source *managedinstance.SummaryResult, dashboardRange ManagedDashboardRange) *managedinstance.SummaryResult {
	result := *source
	result.Window = managedinstance.TimeWindow{Start: dashboardRange.Start, End: dashboardRange.End, Timezone: managedDashboardTimezone}
	result.Trend = make([]managedinstance.UsageTrendPoint, 0, len(source.Trend))
	location, _ := time.LoadLocation(managedDashboardTimezone)
	startDate := time.Unix(dashboardRange.Start, 0).In(location).Format("2006-01-02")
	endDate := time.Unix(dashboardRange.End, 0).In(location).Format("2006-01-02")
	requests, tokens, cost := 0.0, 0.0, 0.0
	for _, point := range source.Trend {
		if point.Date < startDate || point.Date > endDate {
			continue
		}
		result.Trend = append(result.Trend, point)
		requests += point.Requests
		tokens += point.Tokens
		cost += point.Cost
	}
	if len(source.Trend) > 0 {
		result.Requests = managedDashboardMetric(source.Requests, requests)
		result.Tokens = managedDashboardMetric(source.Tokens, tokens)
		result.Cost = managedDashboardMetric(source.Cost, cost)
	}
	return &result
}

func managedDashboardMetric(source managedinstance.MetricSample, value float64) managedinstance.MetricSample {
	source.Value = &value
	source.CollectionStatus = model.ManagedInstanceCollectionSucceeded
	return source
}

func markManagedDashboardPresetFailures(instanceID int64, collectionErr error) {
	for _, days := range managedDashboardPresetDays {
		dashboardRange, _ := NormalizeManagedDashboardRange(days, 0, 0)
		_ = saveManagedDashboardFailure(instanceID, dashboardRange, collectionErr)
	}
}

func saveManagedDashboardSuccess(instanceID int64, dashboardRange ManagedDashboardRange, observedAt int64, summary *managedinstance.SummaryResult) error {
	payload, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	now := common.GetTimestamp()
	values := map[string]any{
		"preset_days": dashboardRange.PresetDays, "window_start": dashboardRange.Start, "window_end": dashboardRange.End,
		"timezone": managedDashboardTimezone, "schema_version": 1, "observed_at": observedAt,
		"etag": hex.EncodeToString(digest[:]), "payload": string(payload), "last_attempt_at": now,
		"last_attempt_status": model.ManagedInstanceCollectionSucceeded, "last_error_code": "", "last_accessed_at": now,
	}
	result := model.DB.Model(&model.ManagedDashboardSnapshot{}).Where("instance_id = ? AND range_key = ?", instanceID, dashboardRange.RangeKey).Updates(values)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		snapshot := model.ManagedDashboardSnapshot{InstanceID: instanceID, RangeKey: dashboardRange.RangeKey, Payload: string(payload)}
		if err := model.DB.Create(&snapshot).Error; err != nil {
			return err
		}
		if err := model.DB.Model(&snapshot).Updates(values).Error; err != nil {
			return err
		}
	}
	section, _ := loadManagedDashboardSection(instanceID, dashboardRange)
	publishManagedDashboardEvent(ManagedDashboardEvent{Type: "summary", InstanceID: instanceID, Snapshot: &section})
	return nil
}

func saveManagedDashboardFailure(instanceID int64, dashboardRange ManagedDashboardRange, collectionErr error) error {
	now := common.GetTimestamp()
	errorCode := managedAccountErrorCode(collectionErr)
	values := map[string]any{
		"preset_days": dashboardRange.PresetDays, "window_start": dashboardRange.Start, "window_end": dashboardRange.End,
		"timezone": managedDashboardTimezone, "last_attempt_at": now,
		"last_attempt_status": model.ManagedInstanceCollectionFailed, "last_error_code": errorCode, "last_accessed_at": now,
	}
	result := model.DB.Model(&model.ManagedDashboardSnapshot{}).Where("instance_id = ? AND range_key = ?", instanceID, dashboardRange.RangeKey).Updates(values)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		snapshot := model.ManagedDashboardSnapshot{InstanceID: instanceID, RangeKey: dashboardRange.RangeKey, Payload: "{}"}
		if err := model.DB.Create(&snapshot).Error; err != nil {
			return err
		}
		if err := model.DB.Model(&snapshot).Updates(values).Error; err != nil {
			return err
		}
	}
	section, _ := loadManagedDashboardSection(instanceID, dashboardRange)
	publishManagedDashboardEvent(ManagedDashboardEvent{Type: "status", InstanceID: instanceID, Snapshot: &section})
	return nil
}

func saveManagedDashboardRunning(instanceID int64, dashboardRange ManagedDashboardRange) error {
	now := common.GetTimestamp()
	values := map[string]any{
		"preset_days": dashboardRange.PresetDays, "window_start": dashboardRange.Start, "window_end": dashboardRange.End,
		"timezone": managedDashboardTimezone, "last_attempt_at": now,
		"last_attempt_status": model.SystemTaskStatusRunning, "last_error_code": "", "last_accessed_at": now,
	}
	result := model.DB.Model(&model.ManagedDashboardSnapshot{}).Where("instance_id = ? AND range_key = ?", instanceID, dashboardRange.RangeKey).Updates(values)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		snapshot := model.ManagedDashboardSnapshot{InstanceID: instanceID, RangeKey: dashboardRange.RangeKey, Payload: "{}"}
		if err := model.DB.Create(&snapshot).Error; err != nil {
			return err
		}
		if err := model.DB.Model(&snapshot).Updates(values).Error; err != nil {
			return err
		}
	}
	section, _ := loadManagedDashboardSection(instanceID, dashboardRange)
	publishManagedDashboardEvent(ManagedDashboardEvent{Type: "status", InstanceID: instanceID, Snapshot: &section})
	return nil
}

func acquireManagedDashboardSlots(ctx context.Context, instanceID int64) (func(), bool, error) {
	key := strconv.FormatInt(instanceID, 10)
	if _, loaded := managedDashboardInFlight.LoadOrStore(key, struct{}{}); loaded {
		return func() {}, false, nil
	}
	global := getManagedDashboardSlots()
	select {
	case global <- struct{}{}:
	case <-ctx.Done():
		managedDashboardInFlight.Delete(key)
		return nil, false, ctx.Err()
	}
	instance, err := managedinstance.Get(instanceID)
	if err != nil {
		<-global
		managedDashboardInFlight.Delete(key)
		return nil, false, err
	}
	hostKey := key
	if parsed, parseErr := url.Parse(instance.BaseURL); parseErr == nil && parsed.Hostname() != "" {
		hostKey = strings.ToLower(parsed.Hostname())
	}
	host := retainManagedInstanceOperationScopedSlot(managedDashboardHostSlots, hostKey, 1)
	select {
	case host.slots <- struct{}{}:
		return func() {
			releaseManagedInstanceOperationScopedSlot(managedDashboardHostSlots, hostKey, host, true)
			<-global
			managedDashboardInFlight.Delete(key)
		}, true, nil
	case <-ctx.Done():
		releaseManagedInstanceOperationScopedSlot(managedDashboardHostSlots, hostKey, host, false)
		<-global
		managedDashboardInFlight.Delete(key)
		return nil, false, ctx.Err()
	}
}

func getManagedDashboardSlots() chan struct{} {
	managedDashboardSlotsOnce.Do(func() { managedDashboardSlots = make(chan struct{}, 2) })
	return managedDashboardSlots
}

func SubscribeManagedDashboard(instanceIDs []int64) (<-chan ManagedDashboardEvent, func()) {
	ids := make(map[int64]struct{}, len(instanceIDs))
	for _, id := range instanceIDs {
		ids[id] = struct{}{}
	}
	subscriber := &managedDashboardSubscriber{ids: ids, events: make(chan ManagedDashboardEvent, 64)}
	managedDashboardSubscribersMu.Lock()
	managedDashboardSubscribers[subscriber] = struct{}{}
	managedDashboardSubscribersMu.Unlock()
	return subscriber.events, func() {
		managedDashboardSubscribersMu.Lock()
		if _, ok := managedDashboardSubscribers[subscriber]; ok {
			delete(managedDashboardSubscribers, subscriber)
			close(subscriber.events)
		}
		managedDashboardSubscribersMu.Unlock()
	}
}

func publishManagedDashboardEvent(event ManagedDashboardEvent) {
	managedDashboardSubscribersMu.RLock()
	defer managedDashboardSubscribersMu.RUnlock()
	for subscriber := range managedDashboardSubscribers {
		if event.InstanceID != 0 {
			if _, ok := subscriber.ids[event.InstanceID]; !ok {
				continue
			}
		}
		select {
		case subscriber.events <- event:
		default:
		}
	}
}
