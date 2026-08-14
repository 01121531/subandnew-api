package service

import (
	"context"
	"sync"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
)

func TestReconcileManagedConductorRealtimeSubscriptionsTracksConfiguredInstances(t *testing.T) {
	truncate(t)
	instances := []model.ManagedInstance{
		{Name: "conductor-one", Kind: model.ManagedInstanceKindConductor, BaseURL: "https://one.example.com"},
		{Name: "new-api", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://new.example.com"},
		{Name: "conductor-two", Kind: model.ManagedInstanceKindConductor, BaseURL: "https://two.example.com"},
	}
	require.NoError(t, model.DB.Create(&instances).Error)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	subscriptions := map[int64]managedConductorRealtimeSubscription{}
	var mu sync.Mutex
	subscribed := map[int64]int{}
	unsubscribed := map[int64]int{}
	subscribe := func(instanceID int64) (<-chan managedinstance.ConductorRealtimeEvent, func(), error) {
		events := make(chan managedinstance.ConductorRealtimeEvent)
		mu.Lock()
		subscribed[instanceID]++
		mu.Unlock()
		var once sync.Once
		return events, func() {
			once.Do(func() {
				mu.Lock()
				unsubscribed[instanceID]++
				mu.Unlock()
				close(events)
			})
		}, nil
	}

	require.NoError(t, reconcileManagedConductorRealtimeSubscriptions(ctx, subscriptions, subscribe))
	require.Len(t, subscriptions, 2)
	require.Equal(t, 1, subscribed[instances[0].Id])
	require.Equal(t, 0, subscribed[instances[1].Id])
	require.Equal(t, 1, subscribed[instances[2].Id])

	require.NoError(t, reconcileManagedConductorRealtimeSubscriptions(ctx, subscriptions, subscribe))
	require.Equal(t, 1, subscribed[instances[0].Id], "an existing stream must be shared")
	require.Equal(t, 1, subscribed[instances[2].Id], "an existing stream must be shared")

	require.NoError(t, model.DB.Model(&model.ManagedInstance{}).Where("id = ?", instances[0].Id).
		Update("kind", model.ManagedInstanceKindNewAPI).Error)
	require.NoError(t, reconcileManagedConductorRealtimeSubscriptions(ctx, subscriptions, subscribe))
	require.Len(t, subscriptions, 1)
	require.Equal(t, 1, unsubscribed[instances[0].Id])
	require.Equal(t, 0, unsubscribed[instances[2].Id])

	closeManagedConductorRealtimeSubscriptions(subscriptions)
	require.Empty(t, subscriptions)
	require.Equal(t, 1, unsubscribed[instances[2].Id])
}
