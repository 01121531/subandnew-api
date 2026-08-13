package managedinstance

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
)

const accountOutputCollectionTimeout = 30 * time.Second
const accountOutputInventorySnapshotTTL = 3 * time.Minute

type AccountOutputItem struct {
	Account          InventoryItem `json:"account"`
	TotalRequests    float64       `json:"total_requests"`
	TotalTokens      float64       `json:"total_tokens"`
	Amount           float64       `json:"amount"`
	Currency         string        `json:"currency"`
	CollectionStatus string        `json:"collection_status"`
	ErrorCode        string        `json:"error_code,omitempty"`
}

type AccountOutputResult struct {
	SourceInstanceID  int64               `json:"source_instance_id"`
	Kind              string              `json:"kind"`
	Window            TimeWindow          `json:"window"`
	Items             []AccountOutputItem `json:"items"`
	AddedAccounts     int                 `json:"added_accounts"`
	CollectedAccounts int                 `json:"collected_accounts"`
	TotalRequests     float64             `json:"total_requests"`
	TotalTokens       float64             `json:"total_tokens"`
	TotalAmount       float64             `json:"total_amount"`
	Currency          string              `json:"currency"`
}

func CollectAccountOutput(ctx context.Context, instanceID int64, window TimeWindow) (*ObservationView, error) {
	instance, adapter, connector, credential, err := observationClient(instanceID)
	if err != nil {
		return nil, err
	}
	if window.End == 0 {
		window.End = common.GetTimestamp()
	}
	if window.Start == 0 {
		window.Start = window.End - 7*86400
	}
	if window.Start < 0 || window.Start >= window.End {
		return nil, ErrInvalidInstance
	}

	resourceKind := defaultResourceKind(instance.Kind)
	page, cached := recentAccountInventorySnapshot(instance.Id, resourceKind)
	inventoryAdapter := adapter
	if !cached {
		if sub2Adapter, ok := adapter.(sub2APIAdapter); ok {
			page, err = sub2Adapter.inventory(ctx, connector, credential, resourceKind, "", false)
			inventoryAdapter = sub2InventoryWithoutUsageAdapter{sub2Adapter}
		} else {
			page, err = adapter.Inventory(ctx, connector, credential, resourceKind, "")
		}
		if err != nil {
			return nil, err
		}
		page, err = collectCompleteInventory(ctx, inventoryAdapter, connector, credential, resourceKind, page)
		if err != nil {
			return nil, err
		}
	}
	items := make([]InventoryItem, 0)
	for _, item := range page.Items {
		if item.CreatedAt >= window.Start && item.CreatedAt <= window.End {
			items = append(items, item)
		}
	}
	result := &AccountOutputResult{
		SourceInstanceID: instance.Id,
		Kind:             instance.Kind,
		Window:           window,
		Items:            make([]AccountOutputItem, len(items)),
		AddedAccounts:    len(items),
	}
	if instance.Kind == model.ManagedInstanceKindConductor {
		for index, item := range items {
			result.Items[index] = AccountOutputItem{Account: item, CollectionStatus: model.ManagedInstanceCollectionUnsupported}
		}
		view, _, viewErr := observationView(instance.Id, common.GetTimestamp(), result, nil)
		return view, viewErr
	}
	usageClient, err := newUsageRecordClient(instance.Id)
	if err != nil {
		return nil, err
	}

	type outputJob struct {
		index int
		item  InventoryItem
	}
	jobs := make(chan outputJob)
	workerCount := min(12, len(items))
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				itemCtx, cancel := context.WithTimeout(ctx, accountOutputCollectionTimeout)
				result.Items[job.index] = collectAccountOutputItem(itemCtx, usageClient, job.item, window)
				cancel()
			}
		}()
	}
	for index, item := range items {
		jobs <- outputJob{index: index, item: item}
	}
	close(jobs)
	workers.Wait()

	for _, item := range result.Items {
		if item.CollectionStatus != model.ManagedInstanceCollectionSucceeded {
			continue
		}
		result.CollectedAccounts++
		result.TotalRequests += item.TotalRequests
		result.TotalTokens += item.TotalTokens
		if result.Currency == "" {
			result.Currency = item.Currency
		}
		if result.Currency == item.Currency {
			result.TotalAmount += item.Amount
		} else {
			result.Currency = "mixed"
			result.TotalAmount = 0
		}
	}
	view, _, err := observationView(instance.Id, common.GetTimestamp(), result, nil)
	return view, err
}

func recentAccountInventorySnapshot(instanceID int64, resourceKind string) (*InventoryPage, bool) {
	var snapshot model.ManagedInstanceSnapshot
	cutoff := common.GetTimestamp() - int64(accountOutputInventorySnapshotTTL/time.Second)
	query := model.DB.Where(
		"instance_id = ? AND snapshot_type = ? AND resource_kind = ? AND collection_status = ? AND observed_at >= ?",
		instanceID,
		model.ManagedInstanceSnapshotTypeInventory,
		resourceKind,
		model.ManagedInstanceCollectionSucceeded,
		cutoff,
	).Limit(1).Find(&snapshot)
	if query.Error != nil || query.RowsAffected == 0 {
		return nil, false
	}
	var page InventoryPage
	if json.Unmarshal([]byte(snapshot.Payload), &page) != nil || page.ResourceKind != resourceKind || page.Total != len(page.Items) {
		return nil, false
	}
	return &page, true
}

func collectAccountOutputItem(ctx context.Context, client *usageRecordClient, item InventoryItem, window TimeWindow) AccountOutputItem {
	result := AccountOutputItem{Account: item, CollectionStatus: model.ManagedInstanceCollectionFailed}
	if client == nil || client.instance == nil {
		result.ErrorCode = managedInstanceObservationErrorCode(ErrInvalidInstance)
		return result
	}
	query := url.Values{}
	if client.instance.Kind == model.ManagedInstanceKindSub2API {
		location, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			location = time.UTC
		}
		query.Set("start_date", time.Unix(window.Start, 0).In(location).Format("2006-01-02"))
		query.Set("end_date", time.Unix(window.End, 0).In(location).Format("2006-01-02"))
		query.Set("timezone", location.String())
		if credentialAccessScope(client.credential) != model.ManagedInstanceAccessUser {
			query.Set("account_id", strconv.FormatInt(item.ID, 10))
		}
	} else {
		query.Set("start_timestamp", strconv.FormatInt(window.Start, 10))
		query.Set("end_timestamp", strconv.FormatInt(window.End, 10))
		if credentialAccessScope(client.credential) != model.ManagedInstanceAccessUser {
			query.Set("channel", strconv.FormatInt(item.ID, 10))
		}
	}
	normalized, err := normalizeUsageRecordQuery(client.instance.Kind, query)
	if err == nil {
		summary, summaryErr := client.summary(ctx, normalized)
		err = summaryErr
		if err == nil {
			result.TotalTokens = summary.TotalTokens
			result.TotalRequests = summary.TotalRequests
			result.Amount = summary.Amount
			result.Currency = summary.Currency
			result.CollectionStatus = model.ManagedInstanceCollectionSucceeded
			return result
		}
	}
	if err != nil {
		result.ErrorCode = managedInstanceObservationErrorCode(err)
		return result
	}
	return result
}
