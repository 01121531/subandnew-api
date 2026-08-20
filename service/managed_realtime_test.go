package service

import (
	"context"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
)

func TestManagedRealtimeSubscriptionSendsHistoricalCacheImmediately(t *testing.T) {
	truncate(t)
	resetManagedRealtimeSubscribersForTest()
	t.Cleanup(resetManagedRealtimeSubscribersForTest)
	instance := model.ManagedInstance{Name: "new-api", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://new.example.com"}
	require.NoError(t, model.DB.Create(&instance).Error)
	require.NoError(t, managedinstance.RecordManagedRPMSample(context.Background(), instance.Id, time.Now().Unix(), 31))

	events, unsubscribe, err := SubscribeManagedRealtime(instance.Id)
	require.NoError(t, err)
	defer unsubscribe()

	status := <-events
	require.Equal(t, "status", status.Type)
	require.Equal(t, instance.Id, status.State.InstanceID)
	require.True(t, status.State.Stale)
	require.Equal(t, "cached", status.State.StreamStatus)
	require.Equal(t, 31.0, *status.State.RPM.Value)
	require.Equal(t, "rpm", (<-events).Type)
}

func TestManagedRealtimeSubscriptionPublishesFullState(t *testing.T) {
	truncate(t)
	resetManagedRealtimeSubscribersForTest()
	t.Cleanup(resetManagedRealtimeSubscribersForTest)
	instance := model.ManagedInstance{Name: "sub2", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://sub2.example.com"}
	require.NoError(t, model.DB.Create(&instance).Error)

	events, unsubscribe, err := SubscribeManagedRealtime(instance.Id)
	require.NoError(t, err)
	defer unsubscribe()
	<-events
	<-events
	rpm := 12.0
	publishManagedRealtimeState("rpm", managedinstance.ManagedRealtimeState{
		InstanceID:   instance.Id,
		ObservedAt:   time.Now().Unix(),
		StreamStatus: "connected",
		RPM:          managedinstance.MetricSample{Value: &rpm, Unit: "request/min", CollectionStatus: model.ManagedInstanceCollectionSucceeded},
	})

	event := <-events
	require.Equal(t, "rpm", event.Type)
	require.Equal(t, 12.0, *event.State.RPM.Value)
}

func TestRefreshManagedRealtimeDeduplicatesInstanceIDsAndInFlightWork(t *testing.T) {
	truncate(t)
	managedPollingRealtimeInFlight.Clear()
	instance := model.ManagedInstance{Name: "new-api", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://new.example.com"}
	require.NoError(t, model.DB.Create(&instance).Error)
	managedPollingRealtimeInFlight.Store(instance.Id, struct{}{})
	t.Cleanup(func() { managedPollingRealtimeInFlight.Delete(instance.Id) })

	views, err := RefreshManagedRealtime([]int64{instance.Id, instance.Id})
	require.NoError(t, err)
	require.Len(t, views, 1)
	require.Equal(t, instance.Id, views[0].InstanceID)
	require.False(t, views[0].Enqueued)
}

func TestRefreshManagedRealtimeRejectsOversizedBatch(t *testing.T) {
	instanceIDs := make([]int64, 101)
	for index := range instanceIDs {
		instanceIDs[index] = int64(index + 1)
	}

	_, err := RefreshManagedRealtime(instanceIDs)
	require.ErrorIs(t, err, managedinstance.ErrInvalidInstance)
}
