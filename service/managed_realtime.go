package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
)

type ManagedRealtimeEvent struct {
	Type            string                               `json:"type"`
	State           managedinstance.ManagedRealtimeState `json:"data"`
	AccountSnapshot *ManagedAccountSnapshotEvent         `json:"account_snapshot,omitempty"`
}

type ManagedAccountSnapshotEvent struct {
	InstanceID        int64    `json:"instance_id"`
	ObservedAt        int64    `json:"observed_at"`
	LastAttemptAt     int64    `json:"last_attempt_at"`
	LastAttemptStatus string   `json:"last_attempt_status"`
	LastErrorCode     string   `json:"last_error_code,omitempty"`
	RangeKeys         []string `json:"range_keys"`
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

var managedRealtimeAccountPublishInterval = 30 * time.Second

type managedRealtimeAccountPublishSlot struct {
	lastPublished time.Time
	pending       *managedinstance.ManagedRealtimeState
	timer         *time.Timer
}

var managedRealtimeAccountPublishes = struct {
	sync.Mutex
	items map[int64]*managedRealtimeAccountPublishSlot
}{items: map[int64]*managedRealtimeAccountPublishSlot{}}

var managedRealtimeForceAccounts sync.Map

func SubscribeManagedRealtime(instanceID int64, topics map[string]struct{}) (<-chan ManagedRealtimeEvent, func(), error) {
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
		for _, topic := range []string{"status", "rpm", "accounts", "sources"} {
			if _, requested := topics[topic]; requested {
				subscriber.events <- ManagedRealtimeEvent{Type: topic, State: state}
			}
		}
	}
	if _, requested := topics["account_snapshot"]; requested {
		if snapshot, snapshotErr := currentManagedAccountSnapshotEvent(instanceID); snapshotErr == nil && snapshot != nil {
			subscriber.events <- ManagedRealtimeEvent{Type: "account_snapshot", AccountSnapshot: snapshot}
		}
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
	if eventType == "accounts" && state.Accounts != nil {
		force := false
		if _, requested := managedRealtimeForceAccounts.LoadAndDelete(state.InstanceID); requested {
			force = true
		}
		publishManagedRealtimeAccounts(state, force)
		return
	}
	broadcastManagedRealtimeState(eventType, state)
}

func publishManagedRealtimeAccounts(state managedinstance.ManagedRealtimeState, force bool) {
	now := time.Now()
	managedRealtimeAccountPublishes.Lock()
	slot := managedRealtimeAccountPublishes.items[state.InstanceID]
	if slot == nil {
		slot = &managedRealtimeAccountPublishSlot{}
		managedRealtimeAccountPublishes.items[state.InstanceID] = slot
	}
	if force || slot.lastPublished.IsZero() || now.Sub(slot.lastPublished) >= managedRealtimeAccountPublishInterval {
		if slot.timer != nil {
			slot.timer.Stop()
			slot.timer = nil
		}
		slot.pending = nil
		slot.lastPublished = now
		managedRealtimeAccountPublishes.Unlock()
		broadcastManagedRealtimeState("accounts", state)
		return
	}

	latest := state
	slot.pending = &latest
	if slot.timer == nil {
		remaining := managedRealtimeAccountPublishInterval - now.Sub(slot.lastPublished)
		var timer *time.Timer
		timer = time.AfterFunc(remaining, func() {
			managedRealtimeAccountPublishes.Lock()
			current := managedRealtimeAccountPublishes.items[state.InstanceID]
			if current == nil || current.timer != timer || current.pending == nil {
				managedRealtimeAccountPublishes.Unlock()
				return
			}
			pending := *current.pending
			current.pending = nil
			current.timer = nil
			current.lastPublished = time.Now()
			managedRealtimeAccountPublishes.Unlock()
			broadcastManagedRealtimeState("accounts", pending)
		})
		slot.timer = timer
	}
	managedRealtimeAccountPublishes.Unlock()
}

func broadcastManagedRealtimeState(eventType string, state managedinstance.ManagedRealtimeState) {
	broadcastManagedRealtimeEvent(ManagedRealtimeEvent{Type: eventType, State: state})
}

func broadcastManagedRealtimeEvent(event ManagedRealtimeEvent) {
	instanceID := event.State.InstanceID
	if event.AccountSnapshot != nil {
		instanceID = event.AccountSnapshot.InstanceID
	}
	managedRealtimeSubscribers.RLock()
	defer managedRealtimeSubscribers.RUnlock()
	for subscriber := range managedRealtimeSubscribers.items {
		if subscriber.instanceID != instanceID {
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

func ManagedRealtimeEventPayload(event ManagedRealtimeEvent) map[string]any {
	if event.Type == "account_snapshot" && event.AccountSnapshot != nil {
		return map[string]any{
			"instance_id":      event.AccountSnapshot.InstanceID,
			"account_snapshot": event.AccountSnapshot,
		}
	}
	state := event.State
	payload := map[string]any{
		"instance_id":     state.InstanceID,
		"observed_at":     state.ObservedAt,
		"last_attempt_at": state.LastAttemptAt,
		"stream_status":   state.StreamStatus,
		"stale":           state.Stale,
		"error_code":      state.ErrorCode,
	}
	switch event.Type {
	case "rpm":
		payload["rpm"] = state.RPM
		payload["rpm_capacity"] = state.RPMCapacity
		payload["success_rate"] = state.SuccessRate
		payload["success_rate_sample_count"] = state.SuccessRateSampleCount
		payload["today_cost"] = state.TodayCost
		payload["today_cost_observed_at"] = state.TodayCostObservedAt
		payload["today_cost_stale"] = state.TodayCostStale
		payload["cost_7d"] = state.Cost7D
		payload["cost_7d_observed_at"] = state.Cost7DObservedAt
		payload["cost_7d_stale"] = state.Cost7DStale
		payload["cost_30d"] = state.Cost30D
		payload["cost_30d_observed_at"] = state.Cost30DObservedAt
		payload["cost_30d_stale"] = state.Cost30DStale
		payload["concurrency_used"] = state.ConcurrencyUsed
		payload["concurrency_max"] = state.ConcurrencyMax
		payload["concurrency_collection_status"] = state.ConcurrencyStatus
	case "accounts":
		payload["accounts_total"] = state.AccountsTotal
		payload["accounts_available"] = state.AccountsAvailable
		payload["accounts_rate_limited"] = state.AccountsRateLimited
		payload["accounts_collection_status"] = state.AccountsCollectionStatus
		payload["accounts_reporting"] = state.AccountsReporting
		payload["active_sessions"] = state.ActiveSessions
		if state.Accounts != nil {
			payload["accounts"] = state.Accounts
		}
	case "sources":
		if state.Sources != nil {
			payload["sources"] = state.Sources
		}
	}
	return payload
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
		if target.Kind == model.ManagedInstanceKindConductor || target.Kind == model.ManagedInstanceKindClaudeGateway {
			managedRealtimeForceAccounts.Store(instanceID, struct{}{})
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
	managedRealtimeAccountPublishes.Lock()
	for instanceID, slot := range managedRealtimeAccountPublishes.items {
		if slot.timer != nil {
			slot.timer.Stop()
		}
		delete(managedRealtimeAccountPublishes.items, instanceID)
	}
	managedRealtimeAccountPublishes.Unlock()
	managedRealtimeForceAccounts.Clear()
}
