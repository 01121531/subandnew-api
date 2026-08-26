package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
)

func TestManagedPollingRealtimeCollectorSelectsSupportedInstancesAndLimitsConcurrency(t *testing.T) {
	truncate(t)
	managedPollingRealtimeInFlight.Clear()
	instances := []model.ManagedInstance{
		{Name: "sub2-one", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://one.example.com"},
		{Name: "new-api", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://new.example.com"},
		{Name: "huichuan", Kind: model.ManagedInstanceKindHuichuan, BaseURL: "https://huichuan.example.com"},
		{Name: "conductor", Kind: model.ManagedInstanceKindConductor, BaseURL: "https://conductor.example.com"},
		{Name: "sub2-two", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://two.example.com"},
	}
	require.NoError(t, model.DB.Create(&instances).Error)

	targets := managedPollingRealtimeTargets(context.Background())
	require.Len(t, targets, 4)
	require.Equal(t, []int64{instances[0].Id, instances[1].Id, instances[2].Id, instances[4].Id}, []int64{
		targets[0].InstanceID,
		targets[1].InstanceID,
		targets[2].InstanceID,
		targets[3].InstanceID,
	})

	var active atomic.Int32
	var maxActive atomic.Int32
	var mu sync.Mutex
	refreshed := map[int64]int{}
	refreshManagedRealtimeTargetsWith(context.Background(), targets, func(_ context.Context, target managedRealtimeTarget) (managedinstance.ManagedRealtimeState, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maxActive.Load()
			if current <= previous || maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		refreshed[target.InstanceID]++
		mu.Unlock()
		return managedinstance.ManagedRealtimeState{InstanceID: target.InstanceID}, nil
	})

	require.LessOrEqual(t, maxActive.Load(), int32(managedPollingRealtimeWorkers))
	for _, target := range targets {
		require.Equal(t, 1, refreshed[target.InstanceID])
	}
}

func TestManagedPollingRealtimeCollectorRecordsSuccessfulRPM(t *testing.T) {
	truncate(t)
	managedPollingRealtimeInFlight.Clear()
	instance := model.ManagedInstance{Name: "sub2", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://sub2.example.com"}
	require.NoError(t, model.DB.Create(&instance).Error)
	rpm := 37.0
	observedAt := time.Now().Unix()

	refreshManagedRealtimeTargetsWith(context.Background(), []managedRealtimeTarget{{InstanceID: instance.Id, Kind: instance.Kind, BaseURL: instance.BaseURL}}, func(_ context.Context, target managedRealtimeTarget) (managedinstance.ManagedRealtimeState, error) {
		return managedinstance.ManagedRealtimeState{
			InstanceID: target.InstanceID,
			ObservedAt: observedAt,
			RPM: managedinstance.MetricSample{
				Value:            &rpm,
				Unit:             "request/min",
				CollectionStatus: model.ManagedInstanceCollectionSucceeded,
			},
		}, nil
	})

	var history model.ManagedRPMHistory
	require.NoError(t, model.DB.Where("instance_id = ?", instance.Id).First(&history).Error)
	require.Equal(t, rpm, history.RPMSum)
	require.Equal(t, 1, history.SampleCount)
}

func TestManagedPollingRealtimeCollectorRecordsGatewayAccountsOnlyOnSuccess(t *testing.T) {
	truncate(t)
	managedPollingRealtimeInFlight.Clear()
	instance := model.ManagedInstance{Name: "gateway", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway.example.com"}
	require.NoError(t, model.DB.Create(&instance).Error)
	rpm := 37.0
	observedAt := time.Now().Unix()
	target := managedRealtimeTarget{InstanceID: instance.Id, Kind: instance.Kind, BaseURL: instance.BaseURL}

	refreshManagedRealtimeTargetsWith(context.Background(), []managedRealtimeTarget{target}, func(_ context.Context, target managedRealtimeTarget) (managedinstance.ManagedRealtimeState, error) {
		return managedinstance.ManagedRealtimeState{
			InstanceID: target.InstanceID,
			ObservedAt: observedAt,
			RPM: managedinstance.MetricSample{
				Value:            &rpm,
				Unit:             "request/min",
				CollectionStatus: model.ManagedInstanceCollectionSucceeded,
			},
			AccountsAvailable:        0,
			AccountsTotal:            0,
			AccountsCollectionStatus: model.ManagedInstanceCollectionSucceeded,
		}, nil
	})

	var history model.ManagedRPMHistory
	require.NoError(t, model.DB.Where("instance_id = ?", instance.Id).First(&history).Error)
	require.Equal(t, 1, history.AccountSampleCount)
	require.Zero(t, history.AccountsAvailableLast)
	require.Zero(t, history.AccountsTotalLast)

	refreshManagedRealtimeTargetsWith(context.Background(), []managedRealtimeTarget{target}, func(_ context.Context, target managedRealtimeTarget) (managedinstance.ManagedRealtimeState, error) {
		return managedinstance.ManagedRealtimeState{
			InstanceID: target.InstanceID,
			ObservedAt: observedAt + 10,
			RPM: managedinstance.MetricSample{
				Value:            &rpm,
				Unit:             "request/min",
				CollectionStatus: model.ManagedInstanceCollectionSucceeded,
			},
			AccountsAvailable:        9,
			AccountsTotal:            10,
			AccountsCollectionStatus: model.ManagedInstanceCollectionSucceeded,
		}, managedinstance.ErrRemoteDataUnavailable
	})

	require.NoError(t, model.DB.Where("instance_id = ?", instance.Id).First(&history).Error)
	require.Equal(t, 1, history.AccountSampleCount)
	require.Zero(t, history.AccountsAvailableLast)
	require.Zero(t, history.AccountsTotalLast)
}
