package managedinstance

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
)

type Sub2RealtimeState struct {
	InstanceID               int64        `json:"instance_id"`
	ObservedAt               int64        `json:"observed_at"`
	LastAttemptAt            int64        `json:"last_attempt_at"`
	DetailsObservedAt        int64        `json:"details_observed_at"`
	LastDetailsAttemptAt     int64        `json:"last_details_attempt_at"`
	RPM                      MetricSample `json:"rpm"`
	TodayCost                MetricSample `json:"today_cost"`
	AccountsTotal            int          `json:"accounts_total"`
	AccountsAvailable        int          `json:"accounts_available"`
	AccountsRateLimited      int          `json:"accounts_rate_limited"`
	AccountsCollectionStatus string       `json:"accounts_collection_status,omitempty"`
	Stale                    bool         `json:"stale"`
	ErrorCode                string       `json:"error_code,omitempty"`
}

const sub2RealtimeDetailsInterval = 60 * time.Second

type sub2RealtimeDetails struct {
	AccountsTotal       int
	AccountsAvailable   int
	AccountsRateLimited int
	AccountsErr         error
	TodayCost           MetricSample
	TodayCostErr        error
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
	sub2RealtimeCache.RLock()
	previous := sub2RealtimeCache.states[instanceID]
	refreshDetails := previous.LastDetailsAttemptAt == 0 || now-previous.LastDetailsAttemptAt >= int64(sub2RealtimeDetailsInterval/time.Second)
	sub2RealtimeCache.RUnlock()
	var details sub2RealtimeDetails
	if refreshDetails {
		details = collectSub2RealtimeDetails(ctx, connector, credential)
	}
	sub2RealtimeCache.Lock()
	defer sub2RealtimeCache.Unlock()

	state := sub2RealtimeCache.states[instanceID]
	state.InstanceID = instanceID
	state.LastAttemptAt = now
	if refreshDetails {
		state.LastDetailsAttemptAt = now
		detailsSucceeded := false
		if details.AccountsErr == nil {
			state.AccountsTotal = details.AccountsTotal
			state.AccountsAvailable = details.AccountsAvailable
			state.AccountsRateLimited = details.AccountsRateLimited
			state.AccountsCollectionStatus = model.ManagedInstanceCollectionSucceeded
			detailsSucceeded = true
		} else if state.AccountsCollectionStatus == "" {
			state.AccountsCollectionStatus = model.ManagedInstanceCollectionFailed
		}
		if details.TodayCostErr == nil {
			state.TodayCost = details.TodayCost
			detailsSucceeded = true
		} else if state.TodayCost.Value == nil {
			state.TodayCost = MetricSample{Unit: "usd", CollectionStatus: model.ManagedInstanceCollectionFailed}
		}
		if detailsSucceeded {
			state.DetailsObservedAt = now
		}
	}
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

func collectSub2RealtimeDetails(ctx context.Context, connector *Connector, credential *CredentialMaterial) sub2RealtimeDetails {
	var result sub2RealtimeDetails
	var requests sync.WaitGroup
	requests.Add(2)
	go func() {
		defer requests.Done()
		adapter := sub2APIAdapter{}
		first, err := adapter.inventory(ctx, connector, credential, "account", "", false)
		if err == nil {
			first, err = collectCompleteInventory(ctx, sub2InventoryWithoutUsageAdapter{adapter}, connector, credential, "account", first)
		}
		if err != nil {
			result.AccountsErr = err
			return
		}
		result.AccountsTotal = len(first.Items)
		for _, item := range first.Items {
			if item.Enabled != nil && *item.Enabled && !item.RateLimited {
				result.AccountsAvailable++
			}
			if item.RateLimited {
				result.AccountsRateLimited++
			}
		}
	}()
	go func() {
		defer requests.Done()
		location, timezone := summaryLocation("Asia/Shanghai")
		today := time.Now().In(location).Format("2006-01-02")
		stats, err := fetchSub2UsageStats(ctx, connector, credential, url.Values{
			"start_date": {today},
			"end_date":   {today},
			"timezone":   {timezone},
		})
		if err != nil {
			result.TodayCostErr = err
			return
		}
		result.TodayCost = supportedMetric(stats.TotalActualCost, "usd")
	}()
	requests.Wait()
	return result
}

func CurrentSub2Realtime(instanceID int64) (Sub2RealtimeState, bool) {
	sub2RealtimeCache.RLock()
	defer sub2RealtimeCache.RUnlock()
	state, ok := sub2RealtimeCache.states[instanceID]
	return state, ok && (state.RPM.Value != nil || state.TodayCost.Value != nil || state.AccountsCollectionStatus == model.ManagedInstanceCollectionSucceeded)
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
		RPM:                      state.RPM,
		TodayCost:                state.TodayCost,
		AccountsTotal:            state.AccountsTotal,
		AccountsAvailable:        state.AccountsAvailable,
		AccountsRateLimited:      state.AccountsRateLimited,
		AccountsCollectionStatus: state.AccountsCollectionStatus,
		StreamStatus:             status,
		Stale:                    state.Stale,
	}, max(state.ObservedAt, state.DetailsObservedAt), true
}

func resetSub2RealtimeCacheForTest() {
	sub2RealtimeCache.Lock()
	sub2RealtimeCache.states = map[int64]Sub2RealtimeState{}
	sub2RealtimeCache.Unlock()
}
