package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
)

type ManagedRealtimeEvent struct {
	Type  string                               `json:"type"`
	State managedinstance.ManagedRealtimeState `json:"data"`
}

type ManagedRealtimeRefreshView struct {
	InstanceID int64 `json:"instance_id"`
	Enqueued   bool  `json:"enqueued"`
}

type managedRealtimeSubscriber struct {
	instanceID int64
	events     chan ManagedRealtimeEvent
}

var managedRealtimeSubscribers = struct {
	sync.RWMutex
	items map[*managedRealtimeSubscriber]struct{}
}{items: map[*managedRealtimeSubscriber]struct{}{}}

func SubscribeManagedRealtime(instanceID int64) (<-chan ManagedRealtimeEvent, func(), error) {
	instance, err := managedinstance.Get(instanceID)
	if err != nil {
		return nil, nil, err
	}
	if instance.Kind == model.ManagedInstanceKindGeneric {
		return nil, nil, managedinstance.ErrUnsupportedCapability
	}
	subscriber := &managedRealtimeSubscriber{instanceID: instanceID, events: make(chan ManagedRealtimeEvent, 32)}
	managedRealtimeSubscribers.Lock()
	managedRealtimeSubscribers.items[subscriber] = struct{}{}
	managedRealtimeSubscribers.Unlock()
	if state, ok, stateErr := managedinstance.CurrentManagedRealtime(instanceID); stateErr == nil {
		if !ok {
			state = managedinstance.ManagedRealtimeState{
				InstanceID: instanceID, StreamStatus: "connecting", Stale: true,
				RPM: managedinstance.MetricSample{Unit: "request/min", CollectionStatus: model.ManagedInstanceCollectionUnsupported},
			}
		}
		subscriber.events <- ManagedRealtimeEvent{Type: "status", State: state}
		subscriber.events <- ManagedRealtimeEvent{Type: "rpm", State: state}
	}
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			managedRealtimeSubscribers.Lock()
			if _, exists := managedRealtimeSubscribers.items[subscriber]; exists {
				delete(managedRealtimeSubscribers.items, subscriber)
				close(subscriber.events)
			}
			managedRealtimeSubscribers.Unlock()
		})
	}
	return subscriber.events, unsubscribe, nil
}

func publishManagedRealtimeState(eventType string, state managedinstance.ManagedRealtimeState) {
	event := ManagedRealtimeEvent{Type: eventType, State: state}
	managedRealtimeSubscribers.RLock()
	defer managedRealtimeSubscribers.RUnlock()
	for subscriber := range managedRealtimeSubscribers.items {
		if subscriber.instanceID != state.InstanceID {
			continue
		}
		select {
		case subscriber.events <- event:
		default:
			select {
			case <-subscriber.events:
			default:
			}
			select {
			case subscriber.events <- event:
			default:
			}
		}
	}
}

func RefreshManagedRealtime(instanceIDs []int64) ([]ManagedRealtimeRefreshView, error) {
	if len(instanceIDs) == 0 || len(instanceIDs) > 100 {
		return nil, fmt.Errorf("%w: realtime refresh requires 1 to 100 instances", managedinstance.ErrInvalidInstance)
	}
	views := make([]ManagedRealtimeRefreshView, 0, len(instanceIDs))
	seen := make(map[int64]struct{}, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		if instanceID <= 0 {
			return nil, managedinstance.ErrInvalidInstance
		}
		if _, duplicate := seen[instanceID]; duplicate {
			continue
		}
		seen[instanceID] = struct{}{}
		target, err := managedRealtimeTargetByID(context.Background(), instanceID)
		if err != nil {
			return nil, err
		}
		enqueued := enqueueManagedRealtimeTarget(target)
		views = append(views, ManagedRealtimeRefreshView{InstanceID: instanceID, Enqueued: enqueued})
	}
	return views, nil
}

func resetManagedRealtimeSubscribersForTest() {
	managedRealtimeSubscribers.Lock()
	for subscriber := range managedRealtimeSubscribers.items {
		close(subscriber.events)
		delete(managedRealtimeSubscribers.items, subscriber)
	}
	managedRealtimeSubscribers.Unlock()
}
