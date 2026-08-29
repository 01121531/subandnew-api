package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/access"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
)

const (
	assistantUsageDefaultPageSize = 20
	assistantUsageMaxPageSize     = 100
	assistantUsageMaxSummaryScope = 20
	assistantUsageQueryTimeout    = 30 * time.Second
)

type usageQueryFilters struct {
	Usernames             []string `json:"usernames,omitempty"`
	TokenNames            []string `json:"token_names,omitempty"`
	Models                []string `json:"models,omitempty"`
	Channels              []string `json:"channels,omitempty"`
	Groups                []string `json:"groups,omitempty"`
	RequestIDs            []string `json:"request_ids,omitempty"`
	UserIDs               []string `json:"user_ids,omitempty"`
	APIKeyIDs             []string `json:"api_key_ids,omitempty"`
	AccountIDs            []string `json:"account_ids,omitempty"`
	GroupIDs              []string `json:"group_ids,omitempty"`
	RequestTypes          []string `json:"request_types,omitempty"`
	BillingTypes          []string `json:"billing_types,omitempty"`
	BillingModes          []string `json:"billing_modes,omitempty"`
	Stream                *bool    `json:"stream,omitempty"`
	UpstreamModelMismatch *bool    `json:"upstream_model_mismatch,omitempty"`
}

type usageRecordsInput struct {
	InstanceIDs   []int64           `json:"instance_ids,omitempty"`
	InstanceScope string            `json:"instance_scope,omitempty"`
	StartAt       string            `json:"start_at,omitempty"`
	EndAt         string            `json:"end_at,omitempty"`
	Filters       usageQueryFilters `json:"filters,omitempty"`
	SortBy        string            `json:"sort_by,omitempty"`
	SortOrder     string            `json:"sort_order,omitempty"`
	Page          int               `json:"page,omitempty"`
	PageSize      int               `json:"page_size,omitempty"`
}

func (input usageRecordsInput) Validate() error {
	if err := validateInstanceSelection(input.InstanceIDs, input.InstanceScope); err != nil {
		return err
	}
	if input.Page < 0 || input.PageSize < 0 || input.PageSize > assistantUsageMaxPageSize {
		return errors.New("invalid usage record pagination")
	}
	if input.SortOrder != "" && input.SortOrder != "asc" && input.SortOrder != "desc" {
		return errors.New("sort_order must be asc or desc")
	}
	if input.SortBy != "" && input.SortBy != "time" && input.SortBy != "model" && input.SortBy != "requests" && input.SortBy != "tokens" && input.SortBy != "cost" {
		return errors.New("invalid usage record sort")
	}
	return validateUsageFilterLimits(input.Filters)
}

type usageFilterOptionsInput struct {
	InstanceIDs   []int64 `json:"instance_ids,omitempty"`
	InstanceScope string  `json:"instance_scope,omitempty"`
	StartAt       string  `json:"start_at,omitempty"`
	EndAt         string  `json:"end_at,omitempty"`
}

func (input usageFilterOptionsInput) Validate() error {
	return validateInstanceSelection(input.InstanceIDs, input.InstanceScope)
}

type usageSummaryInput struct {
	InstanceIDs   []int64           `json:"instance_ids,omitempty"`
	InstanceScope string            `json:"instance_scope,omitempty"`
	StartAt       string            `json:"start_at,omitempty"`
	EndAt         string            `json:"end_at,omitempty"`
	Filters       usageQueryFilters `json:"filters,omitempty"`
}

func (input usageSummaryInput) Validate() error {
	if err := validateInstanceSelection(input.InstanceIDs, input.InstanceScope); err != nil {
		return err
	}
	return validateUsageFilterLimits(input.Filters)
}

type assistantUsageRecord struct {
	RecordKind       string   `json:"record_kind"`
	OccurredAt       string   `json:"occurred_at,omitempty"`
	User             string   `json:"user,omitempty"`
	UserID           string   `json:"user_id,omitempty"`
	APIKeyName       string   `json:"api_key_name,omitempty"`
	APIKeyID         string   `json:"api_key_id,omitempty"`
	AccountName      string   `json:"account_name,omitempty"`
	AccountID        string   `json:"account_id,omitempty"`
	Model            string   `json:"model,omitempty"`
	UpstreamModel    string   `json:"upstream_model,omitempty"`
	Channel          string   `json:"channel,omitempty"`
	Group            string   `json:"group,omitempty"`
	RequestType      string   `json:"request_type,omitempty"`
	RequestID        string   `json:"request_id,omitempty"`
	Requests         *float64 `json:"requests,omitempty"`
	InputTokens      *float64 `json:"input_tokens,omitempty"`
	OutputTokens     *float64 `json:"output_tokens,omitempty"`
	CacheReadTokens  *float64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *float64 `json:"cache_write_tokens,omitempty"`
	TotalTokens      *float64 `json:"total_tokens,omitempty"`
	Amount           *float64 `json:"amount,omitempty"`
	AmountUnit       string   `json:"amount_unit,omitempty"`
	DurationMS       *float64 `json:"duration_ms,omitempty"`
	FirstTokenMS     *float64 `json:"first_token_ms,omitempty"`
	Stream           *bool    `json:"stream,omitempty"`
}

type usageRecordsOutput struct {
	InstanceID   int64                  `json:"instance_id"`
	InstanceName string                 `json:"instance_name"`
	Platform     string                 `json:"platform"`
	Status       string                 `json:"status"`
	ErrorCode    string                 `json:"error_code,omitempty"`
	StartAt      string                 `json:"start_at"`
	EndAt        string                 `json:"end_at"`
	Items        []assistantUsageRecord `json:"items"`
	Total        int64                  `json:"total"`
	Page         int                    `json:"page"`
	PageSize     int                    `json:"page_size"`
	HasMore      bool                   `json:"has_more"`
	TotalIsExact bool                   `json:"total_is_exact"`
	ObservedAt   string                 `json:"observed_at,omitempty"`
}

type assistantUsageFilterOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
type usageFilterOptionsOutput struct {
	InstanceID   int64                                   `json:"instance_id"`
	InstanceName string                                  `json:"instance_name"`
	Platform     string                                  `json:"platform"`
	Status       string                                  `json:"status"`
	ErrorCode    string                                  `json:"error_code,omitempty"`
	Fields       map[string][]assistantUsageFilterOption `json:"fields"`
	ObservedAt   string                                  `json:"observed_at,omitempty"`
}

type usageSummaryItem struct {
	InstanceID    int64    `json:"instance_id"`
	InstanceName  string   `json:"instance_name"`
	Platform      string   `json:"platform"`
	Status        string   `json:"status"`
	ErrorCode     string   `json:"error_code,omitempty"`
	TotalRequests *float64 `json:"total_requests,omitempty"`
	TotalTokens   *float64 `json:"total_tokens,omitempty"`
	Amount        *float64 `json:"amount,omitempty"`
	Currency      string   `json:"currency,omitempty"`
	ObservedAt    string   `json:"observed_at,omitempty"`
}
type usageSummaryTotals struct {
	TotalRequests float64            `json:"total_requests"`
	TotalTokens   float64            `json:"total_tokens"`
	Amounts       map[string]float64 `json:"amounts"`
}
type usageSummaryOutput struct {
	StartAt  string             `json:"start_at"`
	EndAt    string             `json:"end_at"`
	Complete bool               `json:"complete"`
	Items    []usageSummaryItem `json:"items"`
	Totals   usageSummaryTotals `json:"totals"`
}

func registerUsageQueries(registry *tool.Registry, db *gorm.DB) error {
	selection := `"instance_ids":{"type":"array","items":{"type":"integer","minimum":1},"maxItems":100},"instance_scope":{"type":"string","enum":["all"]}`
	timeFields := `"start_at":{"type":"string"},"end_at":{"type":"string"}`
	filterSchema := `"filters":{"type":"object","properties":{"usernames":{"type":"array","items":{"type":"string"},"maxItems":20},"token_names":{"type":"array","items":{"type":"string"},"maxItems":20},"models":{"type":"array","items":{"type":"string"},"maxItems":20},"channels":{"type":"array","items":{"type":"string"},"maxItems":20},"groups":{"type":"array","items":{"type":"string"},"maxItems":20},"request_ids":{"type":"array","items":{"type":"string"},"maxItems":20},"user_ids":{"type":"array","items":{"type":"string"},"maxItems":20},"api_key_ids":{"type":"array","items":{"type":"string"},"maxItems":20},"account_ids":{"type":"array","items":{"type":"string"},"maxItems":20},"group_ids":{"type":"array","items":{"type":"string"},"maxItems":20},"request_types":{"type":"array","items":{"type":"string"},"maxItems":20},"billing_types":{"type":"array","items":{"type":"string"},"maxItems":20},"billing_modes":{"type":"array","items":{"type":"string"},"maxItems":20},"stream":{"type":"boolean"},"upstream_model_mismatch":{"type":"boolean"}},"additionalProperties":false}`
	filterOptionsSchema := json.RawMessage(`{"type":"object","properties":{` + selection + `,` + timeFields + `},"additionalProperties":false}`)
	recordsSchema := json.RawMessage(`{"type":"object","properties":{` + selection + `,` + timeFields + `,` + filterSchema + `,"sort_by":{"type":"string","enum":["time","model","requests","tokens","cost"]},"sort_order":{"type":"string","enum":["asc","desc"]},"page":{"type":"integer","minimum":1},"page_size":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`)
	summarySchema := json.RawMessage(`{"type":"object","properties":{` + selection + `,` + timeFields + `,` + filterSchema + `},"additionalProperties":false}`)
	permission := tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionUsageView}
	if err := tool.Register(registry, tool.ToolSpec{Name: "get_usage_record_filter_options", Version: "v1", Description: "实时读取单个实例使用记录可用的用户、账号、模型、渠道和分组选项。", Permission: permission, Risk: tool.RiskMedium, ReadOnly: true, Idempotent: true, InputSchema: filterOptionsSchema}, func(ctx context.Context, execution tool.ExecutionContext, input usageFilterOptionsInput) (tool.Output[usageFilterOptionsOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
		if err != nil {
			return tool.Output[usageFilterOptionsOutput]{}, err
		}
		if len(resolution.IDs) != 1 {
			return tool.Output[usageFilterOptionsOutput]{}, errors.New("usage record detail tools require exactly one instance")
		}
		return executeUsageFilterOptions(ctx, resolution.IDs[0], input)
	}); err != nil {
		return err
	}
	if err := tool.Register(registry, tool.ToolSpec{Name: "query_usage_records", Version: "v1", Description: "实时限量查询单个实例的使用记录明细，不返回 IP、请求内容、连接信息或凭据。", Permission: permission, Risk: tool.RiskMedium, ReadOnly: true, Idempotent: true, InputSchema: recordsSchema}, func(ctx context.Context, execution tool.ExecutionContext, input usageRecordsInput) (tool.Output[usageRecordsOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
		if err != nil {
			return tool.Output[usageRecordsOutput]{}, err
		}
		if len(resolution.IDs) != 1 {
			return tool.Output[usageRecordsOutput]{}, errors.New("usage record detail tools require exactly one instance")
		}
		return executeUsageRecords(ctx, resolution.IDs[0], input)
	}); err != nil {
		return err
	}
	return tool.Register(registry, tool.ToolSpec{Name: "get_usage_record_summary", Version: "v1", Description: "实时汇总最多 20 个有权实例的使用记录请求数、Token 和费用，按币种分别合计。", Permission: permission, Risk: tool.RiskMedium, ReadOnly: true, Idempotent: true, InputSchema: summarySchema}, func(ctx context.Context, execution tool.ExecutionContext, input usageSummaryInput) (tool.Output[usageSummaryOutput], error) {
		resolution, err := access.ResolveInstanceSelection(ctx, db, execution, input.InstanceIDs, input.InstanceScope)
		if err != nil {
			return tool.Output[usageSummaryOutput]{}, err
		}
		if len(resolution.IDs) > assistantUsageMaxSummaryScope {
			return tool.Output[usageSummaryOutput]{}, errors.New("usage summary supports at most 20 instances")
		}
		return executeUsageSummary(ctx, resolution.IDs, input)
	})
}

func executeUsageFilterOptions(ctx context.Context, instanceID int64, input usageFilterOptionsInput) (tool.Output[usageFilterOptionsOutput], error) {
	instance, err := managedinstance.Get(instanceID)
	if err != nil {
		return tool.Output[usageFilterOptionsOutput]{}, err
	}
	start, end, err := normalizeAssistantUsageRange(input.StartAt, input.EndAt, 31)
	if err != nil {
		return tool.Output[usageFilterOptionsOutput]{}, err
	}
	output := usageFilterOptionsOutput{InstanceID: instanceID, InstanceName: safeBusinessText(instance.Name), Platform: instance.Kind, Status: "unsupported", Fields: map[string][]assistantUsageFilterOption{}}
	provenance := []tool.Provenance{{Source: "managed_usage_records", Resource: "instance:" + strconv.FormatInt(instanceID, 10)}}
	if !assistantUsageSupported(instance.Kind) {
		output.ErrorCode = "unsupported_capability"
		return tool.Output[usageFilterOptionsOutput]{Data: output, Provenance: provenance, Freshness: tool.Freshness{State: tool.FreshnessUnknown, Timezone: assistantTimezone}}, nil
	}
	query := usageRangeValues(instance.Kind, start, end)
	queryCtx, cancel := context.WithTimeout(ctx, assistantUsageQueryTimeout)
	defer cancel()
	result, queryErr := managedinstance.GetUsageRecordFilterOptions(queryCtx, instanceID, query)
	if queryErr != nil {
		output.Status = "failed"
		output.ErrorCode = assistantErrorCode(queryErr)
		return tool.Output[usageFilterOptionsOutput]{Data: output, Provenance: provenance, Freshness: tool.Freshness{State: tool.FreshnessUnknown, Timezone: assistantTimezone}}, nil
	}
	for field, options := range result.Fields {
		values := make([]assistantUsageFilterOption, 0, len(options))
		for _, option := range options {
			values = append(values, assistantUsageFilterOption{Value: safeBusinessText(option.Value), Label: safeBusinessText(option.Label)})
		}
		output.Fields[field] = values
	}
	now := time.Now().In(assistantLocation)
	output.Status = "succeeded"
	output.ObservedAt = now.Format(time.RFC3339)
	provenance[0].ObservedAt = now
	return tool.Output[usageFilterOptionsOutput]{Data: output, Provenance: provenance, Freshness: tool.Freshness{State: tool.FreshnessLive, ObservedAt: now, Timezone: assistantTimezone}}, nil
}

func executeUsageRecords(ctx context.Context, instanceID int64, input usageRecordsInput) (tool.Output[usageRecordsOutput], error) {
	instance, err := managedinstance.Get(instanceID)
	if err != nil {
		return tool.Output[usageRecordsOutput]{}, err
	}
	start, end, err := normalizeAssistantUsageRange(input.StartAt, input.EndAt, 31)
	if err != nil {
		return tool.Output[usageRecordsOutput]{}, err
	}
	page, pageSize := input.Page, input.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = assistantUsageDefaultPageSize
	}
	output := usageRecordsOutput{InstanceID: instanceID, InstanceName: safeBusinessText(instance.Name), Platform: instance.Kind, Status: "unsupported", StartAt: start.Format(time.RFC3339), EndAt: end.Format(time.RFC3339), Items: []assistantUsageRecord{}, Page: page, PageSize: pageSize}
	provenance := []tool.Provenance{{Source: "managed_usage_records", Resource: "instance:" + strconv.FormatInt(instanceID, 10)}}
	if !assistantUsageSupported(instance.Kind) {
		output.ErrorCode = "unsupported_capability"
		return tool.Output[usageRecordsOutput]{Data: output, Provenance: provenance, Freshness: tool.Freshness{State: tool.FreshnessUnknown, Timezone: assistantTimezone}}, nil
	}
	query, err := assistantUsageValues(instance.Kind, start, end, input.Filters)
	if err != nil {
		return tool.Output[usageRecordsOutput]{}, err
	}
	query.Set(pageFieldForAssistant(instance.Kind), strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(pageSize))
	applyAssistantUsageSort(query, instance.Kind, input.SortBy, input.SortOrder)
	queryCtx, cancel := context.WithTimeout(ctx, assistantUsageQueryTimeout)
	defer cancel()
	result, queryErr := managedinstance.ListUsageRecords(queryCtx, instanceID, query)
	if queryErr != nil {
		output.Status = "failed"
		output.ErrorCode = assistantErrorCode(queryErr)
		return tool.Output[usageRecordsOutput]{Data: output, Provenance: provenance, Freshness: tool.Freshness{State: tool.FreshnessUnknown, Timezone: assistantTimezone}}, nil
	}
	for _, raw := range result.Items {
		item, mapErr := assistantUsageRecordFromRaw(instance.Kind, raw)
		if mapErr != nil {
			output.Status = "failed"
			output.ErrorCode = "invalid_response"
			output.Items = nil
			return tool.Output[usageRecordsOutput]{Data: output, Provenance: provenance, Freshness: tool.Freshness{State: tool.FreshnessUnknown, Timezone: assistantTimezone}}, nil
		}
		output.Items = append(output.Items, item)
	}
	now := time.Now().In(assistantLocation)
	output.Status = "succeeded"
	output.Total = result.Total
	output.Page = result.Page
	output.PageSize = result.PageSize
	output.HasMore = result.HasMore
	output.TotalIsExact = result.TotalIsExact
	output.ObservedAt = now.Format(time.RFC3339)
	provenance[0].ObservedAt = now
	return tool.Output[usageRecordsOutput]{Data: output, Provenance: provenance, Freshness: tool.Freshness{State: tool.FreshnessLive, ObservedAt: now, Timezone: assistantTimezone}}, nil
}

func executeUsageSummary(ctx context.Context, instanceIDs []int64, input usageSummaryInput) (tool.Output[usageSummaryOutput], error) {
	start, end, err := normalizeAssistantUsageRange(input.StartAt, input.EndAt, 366)
	if err != nil {
		return tool.Output[usageSummaryOutput]{}, err
	}
	output := usageSummaryOutput{StartAt: start.Format(time.RFC3339), EndAt: end.Format(time.RFC3339), Complete: true, Items: make([]usageSummaryItem, len(instanceIDs)), Totals: usageSummaryTotals{Amounts: map[string]float64{}}}
	provenance := make([]tool.Provenance, 0, len(instanceIDs))
	semaphore := make(chan struct{}, 2)
	var group sync.WaitGroup
	for index, instanceID := range instanceIDs {
		group.Add(1)
		go func(index int, instanceID int64) {
			defer group.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			item := usageSummaryItem{InstanceID: instanceID, Status: "failed"}
			instance, getErr := managedinstance.Get(instanceID)
			if getErr != nil {
				item.ErrorCode = assistantErrorCode(getErr)
				output.Items[index] = item
				return
			}
			item.InstanceName = safeBusinessText(instance.Name)
			item.Platform = instance.Kind
			if !assistantUsageSupported(instance.Kind) {
				item.Status = "unsupported"
				item.ErrorCode = "unsupported_capability"
				output.Items[index] = item
				return
			}
			query, queryErr := assistantUsageValues(instance.Kind, start, end, input.Filters)
			if queryErr != nil {
				item.Status = "unsupported"
				item.ErrorCode = "unsupported_filter"
				output.Items[index] = item
				return
			}
			queryCtx, cancel := context.WithTimeout(ctx, assistantUsageQueryTimeout)
			defer cancel()
			summary, summaryErr := managedinstance.GetUsageRecordSummary(queryCtx, instanceID, query)
			if summaryErr != nil {
				item.ErrorCode = assistantErrorCode(summaryErr)
				output.Items[index] = item
				return
			}
			now := time.Now().In(assistantLocation)
			item.Status = "succeeded"
			item.TotalRequests = &summary.TotalRequests
			item.TotalTokens = &summary.TotalTokens
			item.Amount = &summary.Amount
			item.Currency = summary.Currency
			item.ObservedAt = now.Format(time.RFC3339)
			output.Items[index] = item
		}(index, instanceID)
	}
	group.Wait()
	observedAt := int64(0)
	for _, item := range output.Items {
		if item.Status != "succeeded" {
			output.Complete = false
			continue
		}
		output.Totals.TotalRequests += *item.TotalRequests
		output.Totals.TotalTokens += *item.TotalTokens
		unit := item.Currency
		if unit == "" {
			unit = "unknown"
		}
		output.Totals.Amounts[unit] += *item.Amount
		parsed, _ := time.Parse(time.RFC3339, item.ObservedAt)
		observedAt = conservativeTimestamp(observedAt, parsed.Unix())
		provenance = append(provenance, tool.Provenance{Source: "managed_usage_records", Resource: "instance:" + strconv.FormatInt(item.InstanceID, 10), ObservedAt: parsed})
	}
	if len(provenance) == 0 {
		provenance = []tool.Provenance{{Source: "managed_usage_records"}}
		return tool.Output[usageSummaryOutput]{Data: output, Provenance: provenance, Freshness: tool.Freshness{State: tool.FreshnessUnknown, Timezone: assistantTimezone}}, nil
	}
	state := tool.FreshnessLive
	if !output.Complete {
		state = tool.FreshnessStale
	}
	return tool.Output[usageSummaryOutput]{Data: output, Provenance: provenance, Freshness: tool.Freshness{State: state, ObservedAt: unixTime(observedAt), Timezone: assistantTimezone}}, nil
}

func normalizeAssistantUsageRange(startRaw, endRaw string, maxDays int) (time.Time, time.Time, error) {
	now := time.Now().In(assistantLocation)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, assistantLocation)
	end := start.Add(24*time.Hour - time.Second)
	var err error
	if strings.TrimSpace(startRaw) != "" {
		start, _, err = parseAssistantHistoryTime(startRaw, false)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if strings.TrimSpace(endRaw) != "" {
		end, _, err = parseAssistantHistoryTime(endRaw, true)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if end.Before(start) || end.Sub(start) > time.Duration(maxDays)*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("invalid or oversized usage record time range")
	}
	return start.In(assistantLocation), end.In(assistantLocation), nil
}

func usageRangeValues(kind string, start, end time.Time) url.Values {
	query := url.Values{}
	if kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor {
		query.Set("start_date", start.In(assistantLocation).Format("2006-01-02"))
		query.Set("end_date", end.In(assistantLocation).Format("2006-01-02"))
		query.Set("timezone", assistantTimezone)
	} else {
		query.Set("start_timestamp", strconv.FormatInt(start.Unix(), 10))
		query.Set("end_timestamp", strconv.FormatInt(end.Unix(), 10))
	}
	return query
}

func assistantUsageValues(kind string, start, end time.Time, filters usageQueryFilters) (url.Values, error) {
	query := usageRangeValues(kind, start, end)
	add := func(key string, values []string) {
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				query.Add(key, strings.TrimSpace(value))
			}
		}
	}
	switch kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan:
		add("username", filters.Usernames)
		add("token_name", filters.TokenNames)
		add("model_name", filters.Models)
		add("channel", filters.Channels)
		add("group", filters.Groups)
		add("request_id", filters.RequestIDs)
		if hasSub2OnlyFilters(filters) {
			return nil, errors.New("filters are not supported by New API")
		}
	case model.ManagedInstanceKindSub2API:
		add("user_id", filters.UserIDs)
		add("api_key_id", filters.APIKeyIDs)
		add("account_id", filters.AccountIDs)
		add("group_id", filters.GroupIDs)
		add("model", filters.Models)
		add("request_id", filters.RequestIDs)
		add("request_type", filters.RequestTypes)
		add("billing_type", filters.BillingTypes)
		add("billing_mode", filters.BillingModes)
		if filters.Stream != nil {
			query.Set("stream", strconv.FormatBool(*filters.Stream))
		}
		if filters.UpstreamModelMismatch != nil {
			query.Set("upstream_model_mismatch", strconv.FormatBool(*filters.UpstreamModelMismatch))
		}
		if len(filters.Usernames)+len(filters.TokenNames)+len(filters.Channels)+len(filters.Groups) > 0 {
			return nil, errors.New("filters are not supported by Sub2API")
		}
	case model.ManagedInstanceKindConductor:
		add("user_id", filters.UserIDs)
		add("model", filters.Models)
		if usageFiltersBeyondConductor(filters) {
			return nil, errors.New("filters are not supported by Conductor")
		}
	default:
		return nil, managedinstance.ErrUnsupportedCapability
	}
	return query, nil
}

func validateUsageFilterLimits(filters usageQueryFilters) error {
	groups := [][]string{filters.Usernames, filters.TokenNames, filters.Models, filters.Channels, filters.Groups, filters.RequestIDs, filters.UserIDs, filters.APIKeyIDs, filters.AccountIDs, filters.GroupIDs, filters.RequestTypes, filters.BillingTypes, filters.BillingModes}
	for _, values := range groups {
		if len(values) > 20 {
			return errors.New("too many usage filter values")
		}
		for _, value := range values {
			if strings.TrimSpace(value) == "" || len([]rune(value)) > 512 {
				return errors.New("invalid usage filter value")
			}
		}
	}
	return nil
}

func hasSub2OnlyFilters(f usageQueryFilters) bool {
	return len(f.UserIDs)+len(f.APIKeyIDs)+len(f.AccountIDs)+len(f.GroupIDs)+len(f.RequestTypes)+len(f.BillingTypes)+len(f.BillingModes) > 0 || f.Stream != nil || f.UpstreamModelMismatch != nil
}
func usageFiltersBeyondConductor(f usageQueryFilters) bool {
	return len(f.Usernames)+len(f.TokenNames)+len(f.Channels)+len(f.Groups)+len(f.RequestIDs)+len(f.APIKeyIDs)+len(f.AccountIDs)+len(f.GroupIDs)+len(f.RequestTypes)+len(f.BillingTypes)+len(f.BillingModes) > 0 || f.Stream != nil || f.UpstreamModelMismatch != nil
}
func assistantUsageSupported(kind string) bool {
	return kind == model.ManagedInstanceKindNewAPI || kind == model.ManagedInstanceKindHuichuan || kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor
}
func pageFieldForAssistant(kind string) string {
	if kind == model.ManagedInstanceKindSub2API || kind == model.ManagedInstanceKindConductor {
		return "page"
	}
	return "p"
}
func applyAssistantUsageSort(query url.Values, kind, sortBy, sortOrder string) {
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if kind == model.ManagedInstanceKindSub2API {
		mapping := map[string]string{"time": "created_at", "model": "model"}
		if value := mapping[sortBy]; value != "" {
			query.Set("sort_by", value)
			query.Set("sort_order", sortOrder)
		}
	} else if kind == model.ManagedInstanceKindConductor {
		mapping := map[string]string{"time": "date", "model": "model", "requests": "requests", "tokens": "total_tokens", "cost": "actual_cost"}
		if value := mapping[sortBy]; value != "" {
			query.Set("sort_by", value)
			query.Set("sort_order", sortOrder)
		}
	}
}

func assistantUsageRecordFromRaw(kind string, raw json.RawMessage) (assistantUsageRecord, error) {
	var item map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&item); err != nil {
		return assistantUsageRecord{}, err
	}
	result := assistantUsageRecord{RecordKind: "request"}
	switch kind {
	case model.ManagedInstanceKindSub2API:
		result.OccurredAt = assistantUsageTime(item["created_at"])
		result.User = safeBusinessText(nestedUsageString(item, "user", "email"))
		result.UserID = safeBusinessText(usageString(item["user_id"]))
		result.APIKeyName = safeBusinessText(nestedUsageString(item, "api_key", "name"))
		result.APIKeyID = safeBusinessText(usageString(item["api_key_id"]))
		result.AccountName = safeBusinessText(nestedUsageString(item, "account", "name"))
		result.AccountID = safeBusinessText(usageString(item["account_id"]))
		result.Model = safeBusinessText(usageString(item["model"]))
		result.UpstreamModel = safeBusinessText(usageString(item["upstream_model"]))
		result.Group = safeBusinessText(nestedUsageString(item, "group", "name"))
		if result.Group == "" {
			result.Group = safeBusinessText(usageString(item["group_id"]))
		}
		result.RequestType = safeBusinessText(usageString(item["request_type"]))
		result.RequestID = safeBusinessText(usageString(item["request_id"]))
		result.InputTokens = usageNumberPointer(item["input_tokens"])
		result.OutputTokens = usageNumberPointer(item["output_tokens"])
		result.CacheReadTokens = usageNumberPointer(item["cache_read_tokens"])
		result.CacheWriteTokens = usageNumberPointer(item["cache_creation_tokens"])
		result.TotalTokens = sumUsagePointers(result.InputTokens, result.OutputTokens, result.CacheReadTokens, result.CacheWriteTokens)
		result.Amount = usageNumberPointer(item["actual_cost"])
		result.AmountUnit = "USD"
		result.DurationMS = usageNumberPointer(item["duration_ms"])
		result.FirstTokenMS = usageNumberPointer(item["first_token_ms"])
		result.Stream = usageBoolPointer(item["stream"])
	case model.ManagedInstanceKindConductor:
		result.RecordKind = "daily_aggregate"
		result.OccurredAt = assistantUsageTime(item["date"])
		result.User = safeBusinessText(usageString(item["username"]))
		result.UserID = safeBusinessText(usageString(item["user_id"]))
		result.Model = safeBusinessText(usageString(item["model"]))
		result.Requests = usageNumberPointer(item["requests"])
		result.InputTokens = usageNumberPointer(item["input_tokens"])
		result.OutputTokens = usageNumberPointer(item["output_tokens"])
		result.CacheReadTokens = usageNumberPointer(item["cache_read_tokens"])
		result.CacheWriteTokens = usageNumberPointer(item["cache_creation_tokens"])
		result.TotalTokens = usageNumberPointer(item["total_tokens"])
		result.Amount = usageNumberPointer(item["actual_cost"])
		result.AmountUnit = "USD"
	default:
		result.OccurredAt = assistantUsageTime(item["created_at"])
		result.User = safeBusinessText(usageString(item["username"]))
		result.Model = safeBusinessText(usageString(item["model_name"]))
		result.APIKeyName = safeBusinessText(usageString(item["token_name"]))
		result.Channel = safeBusinessText(usageString(item["channel_name"]))
		if result.Channel == "" {
			result.Channel = safeBusinessText(usageString(item["channel"]))
		}
		result.Group = safeBusinessText(usageString(item["group"]))
		result.RequestType = safeBusinessText(usageString(item["type"]))
		result.RequestID = safeBusinessText(usageString(item["request_id"]))
		result.InputTokens = usageNumberPointer(item["prompt_tokens"])
		result.OutputTokens = usageNumberPointer(item["completion_tokens"])
		result.TotalTokens = sumUsagePointers(result.InputTokens, result.OutputTokens)
		result.Amount = usageNumberPointer(item["quota"])
		result.AmountUnit = "quota"
		if seconds := usageNumberPointer(item["use_time"]); seconds != nil {
			value := *seconds * 1000
			result.DurationMS = &value
		}
		result.Stream = usageBoolPointer(item["is_stream"])
	}
	return result, nil
}

func nestedUsageString(item map[string]any, path ...string) string {
	var current any = item
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[part]
	}
	return usageString(current)
}
func usageString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}
func usageNumberPointer(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		return &typed
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return &parsed
		}
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return &parsed
		}
	}
	return nil
}
func usageBoolPointer(value any) *bool {
	switch typed := value.(type) {
	case bool:
		return &typed
	case string:
		parsed, err := strconv.ParseBool(typed)
		if err == nil {
			return &parsed
		}
	case float64:
		parsed := typed != 0
		return &parsed
	}
	return nil
}
func sumUsagePointers(values ...*float64) *float64 {
	found := false
	total := 0.0
	for _, value := range values {
		if value != nil {
			found = true
			total += *value
		}
	}
	if !found {
		return nil
	}
	return &total
}
func assistantUsageTime(value any) string {
	raw := usageString(value)
	if raw == "" {
		return ""
	}
	if number, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if number > 1_000_000_000_000 {
			number /= 1000
		}
		return assistantTime(number)
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.In(assistantLocation).Format(time.RFC3339)
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, raw, assistantLocation); err == nil {
			return parsed.Format(time.RFC3339)
		}
	}
	return ""
}
