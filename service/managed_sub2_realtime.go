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

const (
	managedSub2RealtimePollInterval      = 10 * time.Second
	managedSub2RealtimeReconcileInterval = time.Minute
	managedSub2RealtimeWorkers           = 2
)

var (
	managedSub2RealtimeCollectorMu   sync.Mutex
	managedSub2RealtimeCollectorStop context.CancelFunc
	managedSub2RealtimeCollectorDone chan struct{}
)

func StartManagedSub2RealtimeCollector() {
	if !common.IsMasterNode || model.DB == nil {
		return
	}
	managedSub2RealtimeCollectorMu.Lock()
	if managedSub2RealtimeCollectorStop != nil {
		managedSub2RealtimeCollectorMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	managedSub2RealtimeCollectorStop = cancel
	managedSub2RealtimeCollectorDone = done
	managedSub2RealtimeCollectorMu.Unlock()

	gopool.Go(func() {
		defer close(done)
		logger.LogInfo(context.Background(), "managed Sub2 realtime collector started")
		instanceIDs := managedSub2RealtimeInstanceIDs(ctx)
		refreshManagedSub2RealtimeInstances(ctx, instanceIDs, managedinstance.RefreshSub2Realtime)

		pollTicker := time.NewTicker(managedSub2RealtimePollInterval)
		reconcileTicker := time.NewTicker(managedSub2RealtimeReconcileInterval)
		defer pollTicker.Stop()
		defer reconcileTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-reconcileTicker.C:
				instanceIDs = managedSub2RealtimeInstanceIDs(ctx)
			case <-pollTicker.C:
				refreshManagedSub2RealtimeInstances(ctx, instanceIDs, managedinstance.RefreshSub2Realtime)
			}
		}
	})
}

func StopManagedSub2RealtimeCollector(ctx context.Context) error {
	managedSub2RealtimeCollectorMu.Lock()
	cancel := managedSub2RealtimeCollectorStop
	done := managedSub2RealtimeCollectorDone
	managedSub2RealtimeCollectorMu.Unlock()
	if cancel == nil || done == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		managedSub2RealtimeCollectorMu.Lock()
		if managedSub2RealtimeCollectorDone == done {
			managedSub2RealtimeCollectorStop = nil
			managedSub2RealtimeCollectorDone = nil
		}
		managedSub2RealtimeCollectorMu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func managedSub2RealtimeInstanceIDs(ctx context.Context) []int64 {
	if model.DB == nil || ctx.Err() != nil {
		return nil
	}
	var instanceIDs []int64
	if err := model.DB.WithContext(ctx).Model(&model.ManagedInstance{}).
		Where("kind = ?", model.ManagedInstanceKindSub2API).
		Order("id asc").Pluck("id", &instanceIDs).Error; err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("managed Sub2 realtime collector reconcile failed: %v", err))
		return nil
	}
	return instanceIDs
}

type managedSub2RealtimeRefreshFunc func(context.Context, int64) (managedinstance.Sub2RealtimeState, error)

func refreshManagedSub2RealtimeInstances(ctx context.Context, instanceIDs []int64, refresh managedSub2RealtimeRefreshFunc) {
	if len(instanceIDs) == 0 || refresh == nil || ctx.Err() != nil {
		return
	}
	jobs := make(chan int64)
	workerCount := min(managedSub2RealtimeWorkers, len(instanceIDs))
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		gopool.Go(func() {
			defer workers.Done()
			for instanceID := range jobs {
				state, err := refresh(ctx, instanceID)
				if err != nil || state.Stale || state.RPM.Value == nil || state.RPM.CollectionStatus != model.ManagedInstanceCollectionSucceeded {
					continue
				}
				observedAt := state.ObservedAt
				if observedAt <= 0 {
					observedAt = common.GetTimestamp()
				}
				_ = managedinstance.RecordManagedRPMSample(ctx, instanceID, observedAt, *state.RPM.Value)
			}
		})
	}
	for _, instanceID := range instanceIDs {
		select {
		case jobs <- instanceID:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}
