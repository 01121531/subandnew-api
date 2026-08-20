package managedinstance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
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
	ConcurrencyUsed          MetricSample `json:"concurrency_used"`
	ConcurrencyMax           MetricSample `json:"concurrency_max"`
	ConcurrencyStatus        string       `json:"concurrency_collection_status,omitempty"`
	TodayCost                MetricSample `json:"today_cost"`
	AccountsTotal            int          `json:"accounts_total"`
	AccountsAvailable        int          `json:"accounts_available"`
	AccountsRateLimited      int          `json:"accounts_rate_limited"`
	AccountsCollectionStatus string       `json:"accounts_collection_status,omitempty"`
	Stale                    bool         `json:"stale"`
	ErrorCode                string       `json:"error_code,omitempty"`
}

const sub2RealtimeDetailsInterval = 60 * time.Second
const sub2RealtimeDetailsRetryInterval = 15 * time.Second
const sub2RealtimeRequestRefreshInterval = 8 * time.Second
const sub2DefaultCapacityGroupID int64 = 49

type sub2RealtimeDetails struct {
	AccountsTotal       int
	AccountsAvailable   int
	AccountsRateLimited int
	AccountsErr         error
	ConcurrencyUsed     MetricSample
	ConcurrencyMax      MetricSample
	ConcurrencyErr      error
	TodayCost           MetricSample
	TodayCostErr        error
}

var sub2RealtimeCache = struct {
	sync.RWMutex
	states map[int64]Sub2RealtimeState
}{states: map[int64]Sub2RealtimeState{}}

var sub2RealtimeRefreshLocks sync.Map

func RefreshSub2Realtime(ctx context.Context, instanceID int64) (Sub2RealtimeState, error) {
	lockValue, _ := sub2RealtimeRefreshLocks.LoadOrStore(instanceID, &sync.Mutex{})
	refreshLock := lockValue.(*sync.Mutex)
	if !refreshLock.TryLock() {
		if state, ok := CurrentSub2Realtime(instanceID); ok {
			return state, nil
		}
		return Sub2RealtimeState{}, ErrUnsupportedCapability
	}
	defer refreshLock.Unlock()

	instance, _, connector, credential, err := observationClient(instanceID)
	if err != nil {
		return storeSub2RealtimeFailure(instanceID, err), err
	}
	if instance.Kind != model.ManagedInstanceKindSub2API {
		return Sub2RealtimeState{}, ErrUnsupportedCapability
	}

	attemptStartedAt := common.GetTimestamp()
	sample := sub2CurrentRPM(ctx, connector, credential)
	sub2RealtimeCache.RLock()
	previous := sub2RealtimeCache.states[instanceID]
	refreshDetails := previous.LastDetailsAttemptAt == 0 || attemptStartedAt-previous.LastDetailsAttemptAt >= int64(sub2RealtimeDetailsInterval/time.Second)
	sub2RealtimeCache.RUnlock()
	var details sub2RealtimeDetails
	if refreshDetails {
		details = collectSub2RealtimeDetails(ctx, connector, credential)
	}
	observedAt := common.GetTimestamp()
	sub2RealtimeCache.Lock()
	defer sub2RealtimeCache.Unlock()

	state := sub2RealtimeCache.states[instanceID]
	state.InstanceID = instanceID
	state.LastAttemptAt = observedAt
	if refreshDetails {
		state.LastDetailsAttemptAt = observedAt
		if details.AccountsErr != nil || details.ConcurrencyErr != nil {
			state.LastDetailsAttemptAt = observedAt - int64((sub2RealtimeDetailsInterval-sub2RealtimeDetailsRetryInterval)/time.Second)
		}
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
		if details.ConcurrencyErr == nil {
			state.ConcurrencyUsed = details.ConcurrencyUsed
			state.ConcurrencyMax = details.ConcurrencyMax
			state.ConcurrencyStatus = model.ManagedInstanceCollectionSucceeded
			detailsSucceeded = true
		} else if state.ConcurrencyStatus == "" {
			state.ConcurrencyStatus = model.ManagedInstanceCollectionFailed
		}
		if detailsSucceeded {
			state.DetailsObservedAt = observedAt
		}
	}
	if sample.Value != nil && sample.CollectionStatus == model.ManagedInstanceCollectionSucceeded {
		state.RPM = sample
		state.ObservedAt = observedAt
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

func storeSub2RealtimeFailure(instanceID int64, err error) Sub2RealtimeState {
	now := common.GetTimestamp()
	sub2RealtimeCache.Lock()
	defer sub2RealtimeCache.Unlock()
	state := sub2RealtimeCache.states[instanceID]
	state.InstanceID = instanceID
	state.LastAttemptAt = now
	state.ErrorCode = managedInstanceObservationErrorCode(err)
	if state.RPM.Value == nil {
		state.RPM = unsupportedMetric("request/min")
		state.Stale = false
	} else {
		state.Stale = true
	}
	sub2RealtimeCache.states[instanceID] = state
	return state
}

func collectSub2RealtimeDetails(ctx context.Context, connector *Connector, credential *CredentialMaterial) sub2RealtimeDetails {
	var result sub2RealtimeDetails
	var requests sync.WaitGroup
	requests.Add(2)
	go func() {
		defer requests.Done()
		total, available, rateLimited, err := fetchSub2GroupAccountCounts(ctx, connector, credential)
		if err != nil {
			total, available, rateLimited, err = collectSub2InventoryAccountCounts(ctx, connector, credential)
		}
		if err != nil {
			result.AccountsErr = err
			return
		}
		result.AccountsTotal = total
		result.AccountsAvailable = available
		result.AccountsRateLimited = rateLimited
	}()
	go func() {
		defer requests.Done()
		used, maximum, err := fetchSub2GroupConcurrency(ctx, connector, credential, sub2DefaultCapacityGroupID)
		if err != nil {
			result.ConcurrencyErr = err
			return
		}
		result.ConcurrencyUsed = supportedMetric(used, "concurrency")
		result.ConcurrencyMax = supportedMetric(maximum, "concurrency")
	}()
	requests.Wait()
	return result
}

func fetchSub2GroupConcurrency(ctx context.Context, connector *Connector, credential *CredentialMaterial, groupID int64) (float64, float64, error) {
	query := url.Values{"timezone": {"Asia/Shanghai"}}
	response, err := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/admin/groups/capacity-summary?"+query.Encode(), nil)
	if err != nil {
		return 0, 0, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return 0, 0, err
	}
	data, err := sub2EnvelopeData(response)
	if err != nil {
		return 0, 0, err
	}
	var groups []struct {
		GroupID         int64   `json:"group_id"`
		ConcurrencyUsed float64 `json:"concurrency_used"`
		ConcurrencyMax  float64 `json:"concurrency_max"`
	}
	if err := json.Unmarshal(data, &groups); err != nil || groups == nil {
		return 0, 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	for _, group := range groups {
		if group.GroupID != groupID {
			continue
		}
		if group.ConcurrencyUsed < 0 || group.ConcurrencyMax < 0 {
			return 0, 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
		}
		return group.ConcurrencyUsed, group.ConcurrencyMax, nil
	}
	return 0, 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
}

func fetchSub2GroupAccountCounts(ctx context.Context, connector *Connector, credential *CredentialMaterial) (int, int, int, error) {
	const pageSize = 100
	totalAccounts := 0
	availableAccounts := 0
	rateLimitedAccounts := 0
	seen := map[int64]struct{}{}
	for pageNumber := 1; pageNumber <= managedInstanceInventoryMaxPages; pageNumber++ {
		query := url.Values{
			"page":       {strconv.Itoa(pageNumber)},
			"page_size":  {strconv.Itoa(pageSize)},
			"status":     {""},
			"sort_by":    {"sort_order"},
			"sort_order": {"asc"},
			"timezone":   {"Asia/Shanghai"},
		}
		response, err := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/admin/groups?"+query.Encode(), nil)
		if err != nil {
			return 0, 0, 0, err
		}
		if err := requireHTTPStatus(response); err != nil {
			return 0, 0, 0, err
		}
		data, err := sub2EnvelopeData(response)
		if err != nil {
			return 0, 0, 0, err
		}
		var page struct {
			Items []struct {
				ID                      int64 `json:"id"`
				AccountCount            int   `json:"account_count"`
				ActiveAccountCount      int   `json:"active_account_count"`
				RateLimitedAccountCount int   `json:"rate_limited_account_count"`
			} `json:"items"`
			Total int `json:"total"`
			Pages int `json:"pages"`
		}
		if json.Unmarshal(data, &page) != nil || page.Items == nil || page.Total < 0 {
			return 0, 0, 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
		}
		for _, group := range page.Items {
			if group.ID <= 0 || group.AccountCount < 0 || group.ActiveAccountCount < 0 || group.RateLimitedAccountCount < 0 {
				return 0, 0, 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
			}
			if _, duplicate := seen[group.ID]; duplicate {
				continue
			}
			seen[group.ID] = struct{}{}
			totalAccounts += group.AccountCount
			availableAccounts += group.ActiveAccountCount
			rateLimitedAccounts += group.RateLimitedAccountCount
		}
		if len(page.Items) == 0 || (page.Pages > 0 && pageNumber >= page.Pages) || len(seen) >= page.Total {
			return totalAccounts, availableAccounts, rateLimitedAccounts, nil
		}
	}
	return 0, 0, 0, &ProbeError{Code: ProbeErrorInvalidResponse}
}

func collectSub2InventoryAccountCounts(ctx context.Context, connector *Connector, credential *CredentialMaterial) (int, int, int, error) {
	adapter := sub2APIAdapter{}
	page, err := adapter.inventory(ctx, connector, credential, "account", "", false)
	if err == nil {
		page, err = collectCompleteInventory(ctx, sub2InventoryWithoutUsageAdapter{adapter}, connector, credential, "account", page)
	}
	if err != nil {
		return 0, 0, 0, err
	}
	available := 0
	rateLimited := 0
	for _, item := range page.Items {
		if item.Enabled != nil && *item.Enabled && !item.RateLimited {
			available++
		}
		if item.RateLimited {
			rateLimited++
		}
	}
	return len(page.Items), available, rateLimited, nil
}

func CurrentSub2Realtime(instanceID int64) (Sub2RealtimeState, bool) {
	sub2RealtimeCache.RLock()
	defer sub2RealtimeCache.RUnlock()
	state, ok := sub2RealtimeCache.states[instanceID]
	return state, ok
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
		ConcurrencyUsed:          state.ConcurrencyUsed,
		ConcurrencyMax:           state.ConcurrencyMax,
		ConcurrencyStatus:        state.ConcurrencyStatus,
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
	sub2RealtimeRefreshLocks = sync.Map{}
}
