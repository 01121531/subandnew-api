package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/logger"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/bytedance/gopkg/util/gopool"
)

const managedConductorRealtimeReconcileInterval = time.Minute

type managedConductorRealtimeSubscription struct {
	unsubscribe func()
}

type managedConductorRealtimeSubscribeFunc func(int64) (<-chan managedinstance.ConductorRealtimeEvent, func(), error)

var (
	managedConductorRealtimeCollectorMu   sync.Mutex
	managedConductorRealtimeCollectorStop context.CancelFunc
	managedConductorRealtimeCollectorDone chan struct{}
)

// StartManagedConductorRealtimeCollector keeps one shared Conductor event stream
// alive per configured instance so RPM history is collected without a browser.
func StartManagedConductorRealtimeCollector() {
	if !common.IsMasterNode || model.DB == nil {
		return
	}
	managedConductorRealtimeCollectorMu.Lock()
	if managedConductorRealtimeCollectorStop != nil {
		managedConductorRealtimeCollectorMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	managedConductorRealtimeCollectorStop = cancel
	managedConductorRealtimeCollectorDone = done
	managedConductorRealtimeCollectorMu.Unlock()

	gopool.Go(func() {
		defer close(done)
		subscriptions := map[int64]managedConductorRealtimeSubscription{}
		defer closeManagedConductorRealtimeSubscriptions(subscriptions)

		reconcile := func() {
			if err := reconcileManagedConductorRealtimeSubscriptions(
				ctx,
				subscriptions,
				managedinstance.SubscribeConductorRealtime,
			); err != nil {
				logger.LogWarn(context.Background(), fmt.Sprintf("managed conductor realtime collector reconcile failed: %v", err))
			}
		}

		logger.LogInfo(context.Background(), "managed conductor realtime collector started")
		reconcile()
		ticker := time.NewTicker(managedConductorRealtimeReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				reconcile()
			}
		}
	})
}

func StopManagedConductorRealtimeCollector(ctx context.Context) error {
	managedConductorRealtimeCollectorMu.Lock()
	cancel := managedConductorRealtimeCollectorStop
	done := managedConductorRealtimeCollectorDone
	managedConductorRealtimeCollectorMu.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		managedConductorRealtimeCollectorMu.Lock()
		if managedConductorRealtimeCollectorDone == done {
			managedConductorRealtimeCollectorStop = nil
			managedConductorRealtimeCollectorDone = nil
		}
		managedConductorRealtimeCollectorMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func reconcileManagedConductorRealtimeSubscriptions(
	ctx context.Context,
	subscriptions map[int64]managedConductorRealtimeSubscription,
	subscribe managedConductorRealtimeSubscribeFunc,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var instanceIDs []int64
	if err := model.DB.WithContext(ctx).Model(&model.ManagedInstance{}).
		Where("kind = ?", model.ManagedInstanceKindConductor).
		Order("id asc").
		Pluck("id", &instanceIDs).Error; err != nil {
		return err
	}

	desired := make(map[int64]struct{}, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		desired[instanceID] = struct{}{}
		if _, exists := subscriptions[instanceID]; exists {
			continue
		}
		events, unsubscribe, err := subscribe(instanceID)
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("managed conductor realtime collector subscribe failed: instance=%d err=%v", instanceID, err))
			continue
		}
		subscriptions[instanceID] = managedConductorRealtimeSubscription{unsubscribe: unsubscribe}
		gopool.Go(func() { drainManagedConductorRealtimeEvents(ctx, events) })
	}

	for instanceID, subscription := range subscriptions {
		if _, keep := desired[instanceID]; keep {
			continue
		}
		if subscription.unsubscribe != nil {
			subscription.unsubscribe()
		}
		delete(subscriptions, instanceID)
	}
	return nil
}

func drainManagedConductorRealtimeEvents(ctx context.Context, events <-chan managedinstance.ConductorRealtimeEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			state, _, err := managedinstance.CurrentManagedRealtime(event.State.InstanceID)
			if err == nil {
				publishManagedRealtimeState(event.Type, state)
			}
		}
	}
}

func closeManagedConductorRealtimeSubscriptions(subscriptions map[int64]managedConductorRealtimeSubscription) {
	for instanceID, subscription := range subscriptions {
		if subscription.unsubscribe != nil {
			subscription.unsubscribe()
		}
		delete(subscriptions, instanceID)
	}
}
