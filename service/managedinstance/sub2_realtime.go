package managedinstance

import (
	"context"
	"sync"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
)

type Sub2RealtimeState struct {
	InstanceID    int64        `json:"instance_id"`
	ObservedAt    int64        `json:"observed_at"`
	LastAttemptAt int64        `json:"last_attempt_at"`
	RPM           MetricSample `json:"rpm"`
	Stale         bool         `json:"stale"`
	ErrorCode     string       `json:"error_code,omitempty"`
}

var sub2RealtimeCache = struct {
	sync.RWMutex
	states map[int64]Sub2RealtimeState
}{states: map[int64]Sub2RealtimeState{}}

func RefreshSub2Realtime(ctx context.Context, instanceID int64) (Sub2RealtimeState, error) {
	instance, _, connector, credential, err := observationClient(instanceID)
	if err != nil {
		return Sub2RealtimeState{}, err
	}
	if instance.Kind != model.ManagedInstanceKindSub2API {
		return Sub2RealtimeState{}, ErrUnsupportedCapability
	}

	now := common.GetTimestamp()
	sample := sub2CurrentRPM(ctx, connector, credential)
	sub2RealtimeCache.Lock()
	defer sub2RealtimeCache.Unlock()

	state := sub2RealtimeCache.states[instanceID]
	state.InstanceID = instanceID
	state.LastAttemptAt = now
	if sample.Value != nil && sample.CollectionStatus == model.ManagedInstanceCollectionSucceeded {
		state.RPM = sample
		state.ObservedAt = now
		state.Stale = false
		state.ErrorCode = ""
		sub2RealtimeCache.states[instanceID] = state
		return state, nil
	}

	if state.RPM.Value == nil {
		state.RPM = unsupportedMetric("request/min")
	}
	state.Stale = state.RPM.Value != nil
	state.ErrorCode = ProbeErrorInvalidResponse
	sub2RealtimeCache.states[instanceID] = state
	return state, ErrUnsupportedCapability
}

func CurrentSub2Realtime(instanceID int64) (Sub2RealtimeState, bool) {
	sub2RealtimeCache.RLock()
	defer sub2RealtimeCache.RUnlock()
	state, ok := sub2RealtimeCache.states[instanceID]
	return state, ok && state.RPM.Value != nil
}

func sub2RealtimeMetrics(instanceID int64) (*RealtimeMetricsResult, int64, bool) {
	state, ok := CurrentSub2Realtime(instanceID)
	if !ok {
		return nil, 0, false
	}
	status := "connected"
	if state.Stale {
		status = "reconnecting"
	}
	return &RealtimeMetricsResult{
		RPM:          state.RPM,
		StreamStatus: status,
		Stale:        state.Stale,
	}, state.ObservedAt, true
}

func resetSub2RealtimeCacheForTest() {
	sub2RealtimeCache.Lock()
	sub2RealtimeCache.states = map[int64]Sub2RealtimeState{}
	sub2RealtimeCache.Unlock()
}
