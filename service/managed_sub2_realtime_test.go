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

func TestManagedSub2RealtimeCollectorSelectsSub2InstancesAndLimitsConcurrency(t *testing.T) {
	truncate(t)
	instances := []model.ManagedInstance{
		{Name: "sub2-one", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://one.example.com"},
		{Name: "new-api", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://new.example.com"},
		{Name: "sub2-two", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://two.example.com"},
		{Name: "sub2-three", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://three.example.com"},
	}
	require.NoError(t, model.DB.Create(&instances).Error)

	instanceIDs := managedSub2RealtimeInstanceIDs(context.Background())
	require.Equal(t, []int64{instances[0].Id, instances[2].Id, instances[3].Id}, instanceIDs)

	var active atomic.Int32
	var maxActive atomic.Int32
	var mu sync.Mutex
	refreshed := map[int64]int{}
	refreshManagedSub2RealtimeInstances(context.Background(), instanceIDs, func(_ context.Context, instanceID int64) (managedinstance.Sub2RealtimeState, error) {
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
		refreshed[instanceID]++
		mu.Unlock()
		return managedinstance.Sub2RealtimeState{InstanceID: instanceID}, nil
	})

	require.LessOrEqual(t, maxActive.Load(), int32(managedSub2RealtimeWorkers))
	require.Equal(t, map[int64]int{instances[0].Id: 1, instances[2].Id: 1, instances[3].Id: 1}, refreshed)
}
