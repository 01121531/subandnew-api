package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/logger"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/bytedance/gopkg/util/gopool"
)

const (
	managedPollingRealtimeInterval          = 10 * time.Second
	managedPollingRealtimeReconcileInterval = time.Minute
	managedPollingRealtimeWorkers           = 2
)

type managedRealtimeTarget struct {
	InstanceID int64
	Kind       string
	BaseURL    string
}

type managedRealtimeRefreshFunc func(context.Context, managedRealtimeTarget) (managedinstance.ManagedRealtimeState, error)

var (
	managedPollingRealtimeCollectorMu   sync.Mutex
	managedPollingRealtimeCollectorStop context.CancelFunc
	managedPollingRealtimeCollectorDone chan struct{}
	managedPollingRealtimeSlotsOnce     sync.Once
	managedPollingRealtimeSlots         chan struct{}
	managedPollingRealtimeHostSlots     = map[string]*managedInstanceOperationScopedSlot{}
	managedPollingRealtimeInFlight      sync.Map
)

func StartManagedPollingRealtimeCollector() {
	if !common.IsMasterNode || model.DB == nil {
		return
	}
	managedPollingRealtimeCollectorMu.Lock()
	if managedPollingRealtimeCollectorStop != nil {
		managedPollingRealtimeCollectorMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	managedPollingRealtimeCollectorStop = cancel
	managedPollingRealtimeCollectorDone = done
	managedPollingRealtimeCollectorMu.Unlock()

	gopool.Go(func() {
		defer close(done)
		logger.LogInfo(context.Background(), "managed polling realtime collector started")
		managedinstance.CleanupManagedRPMHistory(ctx, common.GetTimestamp())
		targets := managedPollingRealtimeTargets(ctx)
		refreshManagedRealtimeTargets(ctx, targets)

		pollTicker := time.NewTicker(managedPollingRealtimeInterval)
		reconcileTicker := time.NewTicker(managedPollingRealtimeReconcileInterval)
		cleanupTicker := time.NewTicker(24 * time.Hour)
		defer pollTicker.Stop()
		defer reconcileTicker.Stop()
		defer cleanupTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-reconcileTicker.C:
				targets = managedPollingRealtimeTargets(ctx)
			case <-pollTicker.C:
				refreshManagedRealtimeTargets(ctx, targets)
			case <-cleanupTicker.C:
				managedinstance.CleanupManagedRPMHistory(ctx, common.GetTimestamp())
			}
		}
	})
}

func StopManagedPollingRealtimeCollector(ctx context.Context) error {
	managedPollingRealtimeCollectorMu.Lock()
	cancel := managedPollingRealtimeCollectorStop
	done := managedPollingRealtimeCollectorDone
	managedPollingRealtimeCollectorMu.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		managedPollingRealtimeCollectorMu.Lock()
		if managedPollingRealtimeCollectorDone == done {
			managedPollingRealtimeCollectorStop = nil
			managedPollingRealtimeCollectorDone = nil
		}
		managedPollingRealtimeCollectorMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func managedPollingRealtimeTargets(ctx context.Context) []managedRealtimeTarget {
	if model.DB == nil || ctx.Err() != nil {
		return nil
	}
	var instances []model.ManagedInstance
	if err := model.DB.WithContext(ctx).
		Where("kind IN ?", []string{model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindMercerRouter, model.ManagedInstanceKindHuichuan, model.ManagedInstanceKindSub2API, model.ManagedInstanceKindClaudeGateway}).
		Order("id asc").Find(&instances).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("managed polling realtime collector reconcile failed: %v", err))
		return nil
	}
	targets := make([]managedRealtimeTarget, 0, len(instances))
	for _, instance := range instances {
		targets = append(targets, managedRealtimeTarget{InstanceID: instance.Id, Kind: instance.Kind, BaseURL: instance.BaseURL})
	}
	return targets
}

func managedRealtimeTargetByID(ctx context.Context, instanceID int64) (managedRealtimeTarget, error) {
	instance, err := managedinstance.Get(instanceID)
	if err != nil {
		return managedRealtimeTarget{}, err
	}
	if instance.Kind == model.ManagedInstanceKindGeneric {
		return managedRealtimeTarget{}, managedinstance.ErrUnsupportedCapability
	}
	return managedRealtimeTarget{InstanceID: instance.Id, Kind: instance.Kind, BaseURL: instance.BaseURL}, nil
}

func refreshManagedRealtimeTargets(ctx context.Context, targets []managedRealtimeTarget) {
	refreshManagedRealtimeTargetsWith(ctx, targets, refreshManagedRealtimeTarget)
}

func refreshManagedRealtimeTargetsWith(ctx context.Context, targets []managedRealtimeTarget, refresh managedRealtimeRefreshFunc) {
	if len(targets) == 0 || ctx.Err() != nil {
		return
	}
	jobs := make(chan managedRealtimeTarget)
	workerCount := min(managedPollingRealtimeWorkers, len(targets))
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		gopool.Go(func() {
			defer workers.Done()
			for target := range jobs {
				runManagedRealtimeTargetWith(ctx, target, refresh)
			}
		})
	}
	for _, target := range targets {
		select {
		case jobs <- target:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}

func enqueueManagedRealtimeTarget(target managedRealtimeTarget) bool {
	if _, loaded := managedPollingRealtimeInFlight.LoadOrStore(target.InstanceID, struct{}{}); loaded {
		return false
	}
	gopool.Go(func() {
		defer managedPollingRealtimeInFlight.Delete(target.InstanceID)
		runManagedRealtimeTargetAcquired(context.Background(), target)
	})
	return true
}

func runManagedRealtimeTarget(ctx context.Context, target managedRealtimeTarget) {
	runManagedRealtimeTargetWith(ctx, target, refreshManagedRealtimeTarget)
}

func runManagedRealtimeTargetWith(ctx context.Context, target managedRealtimeTarget, refresh managedRealtimeRefreshFunc) {
	if _, loaded := managedPollingRealtimeInFlight.LoadOrStore(target.InstanceID, struct{}{}); loaded {
		return
	}
	defer managedPollingRealtimeInFlight.Delete(target.InstanceID)
	runManagedRealtimeTargetAcquiredWith(ctx, target, refresh)
}

func runManagedRealtimeTargetAcquired(ctx context.Context, target managedRealtimeTarget) {
	runManagedRealtimeTargetAcquiredWith(ctx, target, refreshManagedRealtimeTarget)
}

func runManagedRealtimeTargetAcquiredWith(ctx context.Context, target managedRealtimeTarget, refresh managedRealtimeRefreshFunc) {
	global := getManagedPollingRealtimeSlots()
	select {
	case global <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-global }()

	hostKey := fmt.Sprintf("instance:%d", target.InstanceID)
	if parsed, err := url.Parse(target.BaseURL); err == nil && parsed.Hostname() != "" {
		hostKey = strings.ToLower(parsed.Hostname())
	}
	host := retainManagedInstanceOperationScopedSlot(managedPollingRealtimeHostSlots, hostKey, 1)
	select {
	case host.slots <- struct{}{}:
		defer releaseManagedInstanceOperationScopedSlot(managedPollingRealtimeHostSlots, hostKey, host, true)
	case <-ctx.Done():
		releaseManagedInstanceOperationScopedSlot(managedPollingRealtimeHostSlots, hostKey, host, false)
		return
	}

	state, err := refresh(ctx, target)
	eventType := "rpm"
	if err != nil {
		eventType = "status"
	}
	publishManagedRealtimeState(eventType, state)
	if target.Kind == model.ManagedInstanceKindSub2API ||
		target.Kind == model.ManagedInstanceKindClaudeGateway ||
		target.Kind == model.ManagedInstanceKindConductor {
		publishManagedRealtimeState("accounts", state)
	}
	if err == nil && !state.Stale {
		observedAt := state.ObservedAt
		if observedAt <= 0 {
			observedAt = common.GetTimestamp()
		}
		sample := managedinstance.ManagedRealtimeHistorySample{
			RPM: metricValueForHistory(state.RPM), RPMCapacity: metricValueForHistory(state.RPMCapacity),
		}
		if state.ConcurrencyObservedAt == 0 || state.ConcurrencyObservedAt == observedAt {
			sample.ConcurrencyUsed = metricValueForHistory(state.ConcurrencyUsed)
			sample.ConcurrencyMax = metricValueForHistory(state.ConcurrencyMax)
		}
		if !state.TodayCostStale && (state.TodayCostObservedAt == 0 || state.TodayCostObservedAt == observedAt) {
			sample.TodayCost = metricValueForHistory(state.TodayCost)
		}
		if state.SuccessRateObservedAt == observedAt {
			sample.SuccessRate = metricValueForHistory(state.SuccessRate)
			sample.SuccessRateWeight = state.SuccessRateSampleCount
		}
		if state.AccountsCollectionStatus == model.ManagedInstanceCollectionSucceeded && (state.AccountsObservedAt == 0 || state.AccountsObservedAt == observedAt) {
			sample.AccountsAvailable = &state.AccountsAvailable
			sample.AccountsTotal = &state.AccountsTotal
			if target.Kind == model.ManagedInstanceKindClaudeGateway && (state.ActiveSessionsObservedAt == 0 || state.ActiveSessionsObservedAt == observedAt) {
				sample.ActiveSessions = &state.ActiveSessions
			}
		}
		if err := managedinstance.RecordManagedRealtimeHistorySample(ctx, target.InstanceID, observedAt, sample); err != nil {
			managedinstance.ReportManagedRealtimeHistoryWriteError(ctx, target.InstanceID, err)
		}
	}
}

func metricValueForHistory(sample managedinstance.MetricSample) *float64 {
	if sample.CollectionStatus != model.ManagedInstanceCollectionSucceeded || sample.Value == nil {
		return nil
	}
	return sample.Value
}

func refreshManagedRealtimeTarget(ctx context.Context, target managedRealtimeTarget) (managedinstance.ManagedRealtimeState, error) {
	var refreshErr error
	switch target.Kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindMercerRouter, model.ManagedInstanceKindHuichuan:
		state, err := managedinstance.RefreshNewAPIRealtime(ctx, target.InstanceID)
		return state, err
	case model.ManagedInstanceKindSub2API:
		_, refreshErr = managedinstance.RefreshSub2Realtime(ctx, target.InstanceID)
	case model.ManagedInstanceKindClaudeGateway:
		state, err := managedinstance.RefreshClaudeGatewayRealtime(ctx, target.InstanceID)
		return state, err
	case model.ManagedInstanceKindConductor:
		_, refreshErr = managedinstance.RefreshConductorRealtime(ctx, target.InstanceID)
	default:
		return managedinstance.ManagedRealtimeState{}, managedinstance.ErrUnsupportedCapability
	}
	state, _, stateErr := managedinstance.CurrentManagedRealtime(target.InstanceID)
	if stateErr != nil {
		return state, stateErr
	}
	return state, refreshErr
}

func getManagedPollingRealtimeSlots() chan struct{} {
	managedPollingRealtimeSlotsOnce.Do(func() {
		managedPollingRealtimeSlots = make(chan struct{}, managedPollingRealtimeWorkers)
	})
	return managedPollingRealtimeSlots
}
