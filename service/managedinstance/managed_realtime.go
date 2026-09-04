package managedinstance

import (
	"context"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
)

type ManagedRealtimeState struct {
	InstanceID               int64             `json:"instance_id"`
	ObservedAt               int64             `json:"observed_at"`
	LastAttemptAt            int64             `json:"last_attempt_at,omitempty"`
	StreamStatus             string            `json:"stream_status"`
	Stale                    bool              `json:"stale"`
	ErrorCode                string            `json:"error_code,omitempty"`
	RPM                      MetricSample      `json:"rpm"`
	RPMCapacity              MetricSample      `json:"rpm_capacity"`
	SuccessRate              MetricSample      `json:"success_rate"`
	SuccessRateSampleCount   float64           `json:"success_rate_sample_count,omitempty"`
	SuccessRateObservedAt    int64             `json:"success_rate_observed_at,omitempty"`
	ConcurrencyUsed          MetricSample      `json:"concurrency_used"`
	ConcurrencyMax           MetricSample      `json:"concurrency_max"`
	ConcurrencyStatus        string            `json:"concurrency_collection_status,omitempty"`
	ConcurrencyObservedAt    int64             `json:"-"`
	TodayCost                MetricSample      `json:"today_cost"`
	TodayCostObservedAt      int64             `json:"today_cost_observed_at,omitempty"`
	TodayCostStale           bool              `json:"today_cost_stale,omitempty"`
	Cost7D                   MetricSample      `json:"cost_7d"`
	Cost7DObservedAt         int64             `json:"cost_7d_observed_at,omitempty"`
	Cost7DStale              bool              `json:"cost_7d_stale,omitempty"`
	Cost30D                  MetricSample      `json:"cost_30d"`
	Cost30DObservedAt        int64             `json:"cost_30d_observed_at,omitempty"`
	Cost30DStale             bool              `json:"cost_30d_stale,omitempty"`
	AccountsTotal            int               `json:"accounts_total,omitempty"`
	AccountsAvailable        int               `json:"accounts_available,omitempty"`
	AccountsRateLimited      int               `json:"accounts_rate_limited,omitempty"`
	AccountsCollectionStatus string            `json:"accounts_collection_status,omitempty"`
	AccountsObservedAt       int64             `json:"-"`
	AccountsReporting        int               `json:"accounts_reporting,omitempty"`
	ActiveSessions           int               `json:"active_sessions,omitempty"`
	ActiveSessionsObservedAt int64             `json:"-"`
	Accounts                 []InventoryItem   `json:"accounts,omitempty"`
	Sources                  []InventorySource `json:"sources,omitempty"`
}

var newAPIRealtimeCache = struct {
	sync.RWMutex
	states map[int64]ManagedRealtimeState
}{states: map[int64]ManagedRealtimeState{}}

var newAPIRealtimeRefreshLocks sync.Map

func RefreshNewAPIRealtime(ctx context.Context, instanceID int64) (ManagedRealtimeState, error) {
	lockValue, _ := newAPIRealtimeRefreshLocks.LoadOrStore(instanceID, &sync.Mutex{})
	refreshLock := lockValue.(*sync.Mutex)
	if !refreshLock.TryLock() {
		if state, ok := currentNewAPIRealtime(instanceID); ok {
			return state, nil
		}
		return ManagedRealtimeState{}, ErrUnsupportedCapability
	}
	defer refreshLock.Unlock()

	instance, _, connector, credential, err := observationClient(instanceID)
	if err != nil {
		state := markManagedRealtimeFailure(instanceID, ManagedRealtimeState{}, err)
		if previous, ok := currentNewAPIRealtime(instanceID); ok {
			state = markManagedRealtimeFailure(instanceID, previous, err)
		}
		storeNewAPIRealtime(state)
		return state, err
	}
	if instance.Kind != model.ManagedInstanceKindNewAPI && instance.Kind != model.ManagedInstanceKindMercerRouter && instance.Kind != model.ManagedInstanceKindHuichuan {
		return ManagedRealtimeState{}, ErrUnsupportedCapability
	}

	sample, collectionErr := managedPollingCurrentRPM(ctx, connector, instance.Kind, credential)
	if collectionErr != nil && ShouldRecoverDataConnection(collectionErr) {
		if RecoverDataConnection(ctx, instanceID, 0) == nil {
			instance, _, connector, credential, err = observationClient(instanceID)
			if err == nil {
				sample, collectionErr = managedPollingCurrentRPM(ctx, connector, instance.Kind, credential)
			}
		}
	}

	state, _ := currentNewAPIRealtime(instanceID)
	now := common.GetTimestamp()
	state.InstanceID = instanceID
	state.LastAttemptAt = now
	if collectionErr == nil && sample.Value != nil && sample.CollectionStatus == model.ManagedInstanceCollectionSucceeded {
		state.ObservedAt = now
		state.StreamStatus = "connected"
		state.Stale = false
		state.ErrorCode = ""
		state.RPM = sample
		storeNewAPIRealtime(state)
		return state, nil
	}

	if collectionErr == nil {
		collectionErr = ErrRemoteDataUnavailable
	}
	state = markManagedRealtimeFailure(instanceID, state, collectionErr)
	storeNewAPIRealtime(state)
	return state, collectionErr
}

func managedPollingCurrentRPM(ctx context.Context, connector *Connector, kind string, credential *CredentialMaterial) (MetricSample, error) {
	if kind == model.ManagedInstanceKindMercerRouter {
		return mercerRouterCurrentRPM(ctx, connector, credential)
	}
	return newAPICurrentRPM(ctx, connector, kind, credential)
}

func markManagedRealtimeFailure(instanceID int64, state ManagedRealtimeState, err error) ManagedRealtimeState {
	state.InstanceID = instanceID
	state.LastAttemptAt = common.GetTimestamp()
	state.ErrorCode = managedInstanceObservationErrorCode(err)
	if state.RPM.Value == nil {
		state.RPM = unsupportedMetric("request/min")
		state.StreamStatus = "error"
		state.Stale = false
		return state
	}
	state.StreamStatus = "reconnecting"
	state.Stale = true
	return state
}

func CurrentManagedRealtime(instanceID int64) (ManagedRealtimeState, bool, error) {
	instance, err := Get(instanceID)
	if err != nil {
		return ManagedRealtimeState{}, false, err
	}
	switch instance.Kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindMercerRouter, model.ManagedInstanceKindHuichuan:
		state, ok := currentNewAPIRealtime(instanceID)
		return state, ok, nil
	case model.ManagedInstanceKindSub2API:
		if state, ok := CurrentSub2Realtime(instanceID); ok {
			return managedRealtimeFromSub2(state), true, nil
		}
		state, ok := latestManagedRPMState(instanceID)
		return state, ok, nil
	case model.ManagedInstanceKindConductor:
		state, ok := CurrentConductorRealtime(instanceID)
		return managedRealtimeFromConductor(state), ok, nil
	case model.ManagedInstanceKindClaudeGateway:
		state, ok := currentNewAPIRealtime(instanceID)
		return state, ok, nil
	default:
		return ManagedRealtimeState{}, false, ErrUnsupportedCapability
	}
}

func managedRealtimeFromSub2(state Sub2RealtimeState) ManagedRealtimeState {
	status := "connected"
	if state.Stale {
		status = "reconnecting"
	} else if state.RPM.Value == nil && state.ErrorCode != "" {
		status = "error"
	} else if state.RPM.Value == nil {
		status = "connecting"
	}
	return ManagedRealtimeState{
		InstanceID: state.InstanceID, ObservedAt: max(state.ObservedAt, state.DetailsObservedAt), LastAttemptAt: state.LastAttemptAt,
		StreamStatus: status, Stale: state.Stale, ErrorCode: state.ErrorCode, RPM: state.RPM,
		ConcurrencyUsed: state.ConcurrencyUsed, ConcurrencyMax: state.ConcurrencyMax, ConcurrencyStatus: state.ConcurrencyStatus,
		ConcurrencyObservedAt: state.DetailsObservedAt,
		TodayCost:             state.TodayCost, AccountsTotal: state.AccountsTotal, AccountsAvailable: state.AccountsAvailable,
		AccountsRateLimited: state.AccountsRateLimited, AccountsCollectionStatus: state.AccountsCollectionStatus,
		AccountsObservedAt: state.DetailsObservedAt,
	}
}

func managedRealtimeFromConductor(state ConductorRealtimeState) ManagedRealtimeState {
	return ManagedRealtimeState{
		InstanceID: state.InstanceID, ObservedAt: state.ObservedAt, LastAttemptAt: state.ObservedAt,
		StreamStatus: state.StreamStatus, Stale: state.Stale, ErrorCode: state.ErrorCode,
		RPM: state.RPM, RPMCapacity: state.RPMCapacity, TodayCost: state.TodayCost,
		AccountsTotal: state.AccountsTotal, AccountsAvailable: state.AccountsAvailable,
		AccountsRateLimited: state.AccountsRateLimited, AccountsReporting: state.AccountsReporting,
		AccountsCollectionStatus: model.ManagedInstanceCollectionSucceeded,
		AccountsObservedAt:       state.ObservedAt,
		ActiveSessions:           state.ActiveSessions, ActiveSessionsObservedAt: state.ObservedAt, Accounts: state.Accounts, Sources: state.Sources,
	}
}

func (state ManagedRealtimeState) Metrics() *RealtimeMetricsResult {
	return &RealtimeMetricsResult{
		RPM: state.RPM, RPMCapacity: state.RPMCapacity, SuccessRate: state.SuccessRate,
		SuccessRateSampleCount: state.SuccessRateSampleCount,
		ConcurrencyUsed:        state.ConcurrencyUsed, ConcurrencyMax: state.ConcurrencyMax, ConcurrencyStatus: state.ConcurrencyStatus,
		TodayCost: state.TodayCost, Cost7D: state.Cost7D, Cost30D: state.Cost30D,
		AccountsTotal: state.AccountsTotal, AccountsAvailable: state.AccountsAvailable,
		AccountsRateLimited: state.AccountsRateLimited, AccountsCollectionStatus: state.AccountsCollectionStatus,
		AccountsReporting: state.AccountsReporting, ActiveSessions: state.ActiveSessions,
		StreamStatus: state.StreamStatus, Stale: state.Stale,
	}
}

func currentNewAPIRealtime(instanceID int64) (ManagedRealtimeState, bool) {
	newAPIRealtimeCache.RLock()
	state, ok := newAPIRealtimeCache.states[instanceID]
	newAPIRealtimeCache.RUnlock()
	if ok {
		return state, state.RPM.Value != nil
	}
	state, ok = latestManagedRPMState(instanceID)
	if ok {
		storeNewAPIRealtime(state)
	}
	return state, ok
}

func latestManagedRPMState(instanceID int64) (ManagedRealtimeState, bool) {
	if model.DB == nil || instanceID <= 0 || !model.DB.Migrator().HasTable(&model.ManagedRPMHistory{}) {
		return ManagedRealtimeState{}, false
	}
	var history model.ManagedRPMHistory
	query := model.DB.Where(
		"instance_id = ? AND (sample_count > 0 OR today_cost_sample_count > 0 OR cost_7d_sample_count > 0 OR cost_30d_sample_count > 0)",
		instanceID,
	).Order("bucket_start desc").Limit(1).Find(&history)
	if query.Error != nil || query.RowsAffected == 0 {
		return ManagedRealtimeState{}, false
	}
	state := ManagedRealtimeState{
		InstanceID: instanceID, ObservedAt: history.UpdatedAt, LastAttemptAt: history.UpdatedAt,
		StreamStatus: "cached", Stale: true, RPM: unsupportedMetric("request/min"),
	}
	if history.SampleCount > 0 && history.RPMLast >= 0 {
		state.RPM = supportedMetric(history.RPMLast, "request/min")
	}
	if history.SuccessRateSampleCount > 0 && history.SuccessRateLast >= 0 && history.SuccessRateLast <= 1 {
		state.SuccessRate = supportedMetric(history.SuccessRateLast, "ratio")
		state.SuccessRateObservedAt = history.UpdatedAt
	}
	if history.ConcurrencyUsedSamples > 0 {
		state.ConcurrencyUsed = supportedMetric(history.ConcurrencyUsedLast, "concurrency")
	}
	if history.ConcurrencyMaxSamples > 0 {
		state.ConcurrencyMax = supportedMetric(history.ConcurrencyMaxLast, "concurrency")
	}
	if history.ConcurrencyUsedSamples > 0 || history.ConcurrencyMaxSamples > 0 {
		state.ConcurrencyStatus = model.ManagedInstanceCollectionSucceeded
		state.ConcurrencyObservedAt = history.UpdatedAt
	}
	loadLatestManagedCosts(instanceID, &state)
	if history.AccountSampleCount > 0 {
		state.AccountsAvailable = history.AccountsAvailableLast
		state.AccountsTotal = history.AccountsTotalLast
		state.AccountsCollectionStatus = model.ManagedInstanceCollectionSucceeded
		state.AccountsObservedAt = history.UpdatedAt
	}
	if history.ActiveSessionSamples > 0 {
		state.ActiveSessions = history.ActiveSessionsLast
		state.ActiveSessionsObservedAt = history.UpdatedAt
	}
	return state, true
}

func loadLatestManagedCosts(instanceID int64, state *ManagedRealtimeState) {
	if state == nil {
		return
	}
	now := common.GetTimestamp()
	staleAfter := int64((5 * time.Minute) / time.Second)
	load := func(sampleColumn string) (model.ManagedRPMHistory, bool) {
		var history model.ManagedRPMHistory
		query := model.DB.Where("instance_id = ? AND "+sampleColumn+" > 0", instanceID).
			Order("bucket_start desc").Limit(1).Find(&history)
		return history, query.Error == nil && query.RowsAffected > 0
	}
	if history, ok := load("today_cost_sample_count"); ok {
		state.TodayCost = supportedMetric(history.TodayCostLast, "usd")
		state.TodayCostObservedAt = history.UpdatedAt
		state.TodayCostStale = now-history.UpdatedAt > staleAfter
	}
	if history, ok := load("cost_7d_sample_count"); ok {
		state.Cost7D = supportedMetric(history.Cost7DLast, "usd")
		state.Cost7DObservedAt = history.UpdatedAt
		state.Cost7DStale = now-history.UpdatedAt > staleAfter
	}
	if history, ok := load("cost_30d_sample_count"); ok {
		state.Cost30D = supportedMetric(history.Cost30DLast, "usd")
		state.Cost30DObservedAt = history.UpdatedAt
		state.Cost30DStale = now-history.UpdatedAt > staleAfter
	}
}

func storeNewAPIRealtime(state ManagedRealtimeState) {
	newAPIRealtimeCache.Lock()
	newAPIRealtimeCache.states[state.InstanceID] = state
	newAPIRealtimeCache.Unlock()
}

func resetNewAPIRealtimeCacheForTest() {
	newAPIRealtimeCache.Lock()
	newAPIRealtimeCache.states = map[int64]ManagedRealtimeState{}
	newAPIRealtimeCache.Unlock()
	newAPIRealtimeRefreshLocks = sync.Map{}
}
