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
