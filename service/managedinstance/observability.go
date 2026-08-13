package managedinstance

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const managedInstanceSnapshotSchemaVersion = 1

const (
	managedInstanceInventoryPageSize = 100
	managedInstanceInventoryMaxItems = 10000
	managedInstanceInventoryMaxPages = managedInstanceInventoryMaxItems
	managedInstanceInventoryWorkers  = 8
)

type TimeWindow struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type InventoryItem struct {
	ID                   int64    `json:"id"`
	Name                 string   `json:"name"`
	Type                 string   `json:"type,omitempty"`
	Platform             string   `json:"platform,omitempty"`
	Group                string   `json:"group,omitempty"`
	Status               string   `json:"status,omitempty"`
	Enabled              *bool    `json:"enabled,omitempty"`
	CreatedAt            int64    `json:"created_at,omitempty"`
	LastActivityAt       int64    `json:"last_activity_at,omitempty"`
	Requests             *float64 `json:"requests,omitempty"`
	Tokens               *float64 `json:"tokens,omitempty"`
	Cost                 *float64 `json:"cost,omitempty"`
	CostUnit             string   `json:"cost_unit,omitempty"`
	Balance              *float64 `json:"balance,omitempty"`
	ResponseTimeMS       *int64   `json:"response_time_ms,omitempty"`
	ErrorMessage         string   `json:"error_message,omitempty"`
	ActiveSessions       *int     `json:"active_sessions,omitempty"`
	RPM                  *int     `json:"rpm,omitempty"`
	AccountCount         *int     `json:"account_count,omitempty"`
	Utilization5H        *float64 `json:"utilization_5h,omitempty"`
	Utilization7D        *float64 `json:"utilization_7d,omitempty"`
	Utilization7DOI      *float64 `json:"utilization_7d_oi,omitempty"`
	InputPricePerM       *float64 `json:"input_price_per_m,omitempty"`
	OutputPricePerM      *float64 `json:"output_price_per_m,omitempty"`
	CacheReadPricePerM   *float64 `json:"cache_read_price_per_m,omitempty"`
	CacheCreatePricePerM *float64 `json:"cache_create_price_per_m,omitempty"`
}

type InventoryPage struct {
	ResourceKind string          `json:"resource_kind"`
	Items        []InventoryItem `json:"items"`
	Total        int             `json:"total"`
	NextCursor   string          `json:"next_cursor,omitempty"`
}

type ResourceSummary struct {
	ResourceKind string `json:"resource_kind"`
	Total        int    `json:"total"`
	Enabled      *int   `json:"enabled"`
	Unhealthy    *int   `json:"unhealthy"`
}

type MetricSample struct {
	Value            *float64 `json:"value"`
	Unit             string   `json:"unit"`
	CollectionStatus string   `json:"collection_status"`
}

type UsageTrendPoint struct {
	Date     string  `json:"date"`
	Requests float64 `json:"requests"`
	Tokens   float64 `json:"tokens"`
	Cost     float64 `json:"cost"`
}

type SummaryResult struct {
	Window    TimeWindow        `json:"window"`
	Resources []ResourceSummary `json:"resources"`
	Requests  MetricSample      `json:"requests"`
	Tokens    MetricSample      `json:"tokens"`
	Cost      MetricSample      `json:"cost"`
	ErrorRate MetricSample      `json:"error_rate"`
	Latency   MetricSample      `json:"latency"`
	Trend     []UsageTrendPoint `json:"trend"`
}

type RealtimeMetricsResult struct {
	RPM MetricSample `json:"rpm"`
}

type ObservationView struct {
	SourceInstanceID int64  `json:"source_instance_id"`
	ObservedAt       int64  `json:"observed_at"`
	CollectionStatus string `json:"collection_status"`
	ErrorCode        string `json:"error_code,omitempty"`
	ETag             string `json:"etag,omitempty"`
	Data             any    `json:"data,omitempty"`
}

type CommitGuard func(*gorm.DB) error

func (adapter newAPIAdapter) Inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind string, cursor string) (*InventoryPage, error) {
	if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		if strings.TrimSpace(cursor) != "" {
			return nil, ErrInvalidInstance
		}
		response, err := newAPIDoJSON(ctx, connector, adapter.configuredKind, credential, http.MethodGet, "/api/user/self", nil)
		if err != nil {
			return nil, err
		}
		data, err := newAPIEnvelopeData(response)
		if err != nil {
			return nil, err
		}
		var profile struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
			Role     int    `json:"role"`
			Status   int    `json:"status"`
		}
		if json.Unmarshal(data, &profile) != nil || profile.ID <= 0 {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(data, &fields)
		createdAt, _ := firstJSONUnixTime(fields, "created_at", "created_time", "uploaded_at", "upload_time")
		enabled := profile.Status == 1
		return &InventoryPage{
			ResourceKind: "user", Total: 1,
			Items: []InventoryItem{{ID: profile.ID, Name: profile.Username, Type: strconv.Itoa(profile.Role), Status: strconv.Itoa(profile.Status), Enabled: &enabled, CreatedAt: createdAt}},
		}, nil
	}
	resourceKind = normalizeResourceKind(resourceKind, "channel")
	if resourceKind != "channel" {
		return nil, ErrUnsupportedCapability
	}
	pageNumber, err := newAPIPageNumber(cursor)
	if err != nil {
		return nil, err
	}
	response, err := newAPIDoJSON(ctx, connector, adapter.configuredKind, credential, http.MethodGet, newAPIInventoryEndpoint(pageNumber), nil)
	if err != nil {
		return nil, err
	}
	data, err := newAPIEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	page, err := normalizeInventoryPage("channel", data)
	if err != nil {
		return nil, err
	}
	loadedThrough := (pageNumber-1)*managedInstanceInventoryPageSize + len(page.Items)
	if page.NextCursor == "" && loadedThrough < page.Total {
		page.NextCursor = fmt.Sprintf("newapi:%d", pageNumber+1)
	}
	return page, nil
}

func (adapter newAPIAdapter) Summary(ctx context.Context, connector *Connector, credential *CredentialMaterial, window TimeWindow) (*SummaryResult, error) {
	page, err := adapter.Inventory(ctx, connector, credential, "channel", "")
	if err != nil {
		return nil, err
	}
	summary := summaryFromInventory(window, page)

	query := url.Values{}
	query.Set("start_timestamp", strconv.FormatInt(window.Start, 10))
	query.Set("end_timestamp", strconv.FormatInt(window.End, 10))
	endpoint := "/api/data/"
	if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		endpoint = "/api/data/self"
	}
	response, err := newAPIDoJSON(ctx, connector, adapter.configuredKind, credential, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return summary, nil
	}

	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			CreatedAt int64   `json:"created_at"`
			TokenUsed float64 `json:"token_used"`
			Count     float64 `json:"count"`
			Quota     float64 `json:"quota"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil || !payload.Success {
		return summary, nil
	}

	requests := 0.0
	tokens := 0.0
	quota := 0.0
	daily := make(map[string]UsageTrendPoint)
	for _, item := range payload.Data {
		requests += item.Count
		tokens += item.TokenUsed
		quota += item.Quota
		if item.CreatedAt > 0 {
			date := time.Unix(item.CreatedAt, 0).UTC().Format("2006-01-02")
			point := daily[date]
			point.Date = date
			point.Requests += item.Count
			point.Tokens += item.TokenUsed
			point.Cost += item.Quota
			daily[date] = point
		}
	}
	summary.Requests = supportedMetric(requests, "request")
	summary.Tokens = supportedMetric(tokens, "token")
	summary.Cost = supportedMetric(quota, "quota")
	summary.Trend = fillDailyTrend(window, daily)
	return summary, nil
}

func newAPICurrentRPM(ctx context.Context, connector *Connector, configuredKind string, credential *CredentialMaterial) MetricSample {
	now := time.Now()
	query := url.Values{
		"type":            {"2"},
		"start_timestamp": {strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)},
		"end_timestamp":   {strconv.FormatInt(now.Unix(), 10)},
	}
	endpoint := "/api/log/stat"
	if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		endpoint = "/api/log/self/stat"
	}
	response, err := newAPIDoJSON(ctx, connector, configuredKind, credential, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return unsupportedMetric("request/min")
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			RPM float64 `json:"rpm"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Body, &payload) != nil || !payload.Success || payload.Data.RPM < 0 {
		return unsupportedMetric("request/min")
	}
	return supportedMetric(payload.Data.RPM, "request/min")
}

func supportedMetric(value float64, unit string) MetricSample {
	return MetricSample{Value: &value, Unit: unit, CollectionStatus: model.ManagedInstanceCollectionSucceeded}
}

func unsupportedMetric(unit string) MetricSample {
	return MetricSample{Unit: unit, CollectionStatus: model.ManagedInstanceCollectionUnsupported}
}

func (adapter sub2APIAdapter) Inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind string, cursor string) (*InventoryPage, error) {
	return adapter.inventory(ctx, connector, credential, resourceKind, cursor, true)
}

func (adapter sub2APIAdapter) inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind string, cursor string, includeAccountUsage bool) (*InventoryPage, error) {
	if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		if strings.TrimSpace(cursor) != "" {
			return nil, ErrInvalidInstance
		}
		response, err := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/user/profile", nil)
		if err != nil {
			return nil, err
		}
		data, err := sub2EnvelopeData(response)
		if err != nil {
			return nil, err
		}
		var profile struct {
			ID       int64  `json:"id"`
			Email    string `json:"email"`
			Username string `json:"username"`
			Role     string `json:"role"`
			Status   string `json:"status"`
		}
		if json.Unmarshal(data, &profile) != nil || profile.ID <= 0 {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(data, &fields)
		createdAt, _ := firstJSONUnixTime(fields, "created_at", "created_time", "uploaded_at", "upload_time")
		name := strings.TrimSpace(profile.Username)
		if name == "" {
			name = strings.TrimSpace(profile.Email)
		}
		enabled := strings.EqualFold(profile.Status, "active") || strings.EqualFold(profile.Status, "enabled")
		return &InventoryPage{
			ResourceKind: "user", Total: 1,
			Items: []InventoryItem{{ID: profile.ID, Name: name, Type: profile.Role, Status: profile.Status, Enabled: &enabled, CreatedAt: createdAt}},
		}, nil
	}
	resourceKind = normalizeResourceKind(resourceKind, "account")
	if resourceKind != "account" {
		return nil, ErrUnsupportedCapability
	}
	endpoint, pageNumber, err := sub2InventoryEndpoint(cursor)
	if err != nil {
		return nil, err
	}
	response, err := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	data, err := sub2EnvelopeData(response)
	if err != nil {
		return nil, err
	}
	page, err := normalizeInventoryPage("account", data)
	if err != nil {
		return nil, err
	}
	if includeAccountUsage {
		enrichSub2AccountUsage(ctx, connector, credential, page)
	}
	if page.NextCursor == "" {
		page.NextCursor = sub2NextPageCursor(data, pageNumber)
	}
	return page, nil
}

func (adapter sub2APIAdapter) Summary(ctx context.Context, connector *Connector, credential *CredentialMaterial, window TimeWindow) (*SummaryResult, error) {
	page, err := adapter.inventory(ctx, connector, credential, "account", "", false)
	if err != nil {
		return nil, err
	}
	summary := summaryFromInventory(window, page)

	headers, err := sub2APIAuthHeaders(ctx, connector, credential)
	if err != nil {
		return summary, nil
	}
	query := url.Values{}
	query.Set("start_date", time.Unix(window.Start, 0).UTC().Format("2006-01-02"))
	query.Set("end_date", time.Unix(window.End, 0).UTC().Format("2006-01-02"))
	query.Set("timezone", "UTC")
	query.Set("granularity", "day")
	query.Set("include_stats", "false")
	query.Set("include_trend", "true")
	query.Set("include_model_stats", "false")
	query.Set("include_group_stats", "false")
	endpoint := "/api/v1/admin/dashboard/snapshot-v2"
	if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		endpoint = "/api/v1/usage/dashboard/snapshot-v2"
		query.Del("include_stats")
		query.Set("include_trend", "true")
	}
	response, err := connector.DoJSON(ctx, http.MethodGet, endpoint+"?"+query.Encode(), headers, nil)
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return summary, nil
	}

	var payload struct {
		Code any `json:"code"`
		Data struct {
			Trend *[]struct {
				Date        string  `json:"date"`
				Requests    float64 `json:"requests"`
				TotalTokens float64 `json:"total_tokens"`
				ActualCost  float64 `json:"actual_cost"`
			} `json:"trend"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &payload); err != nil || !sub2SuccessCode(payload.Code) || payload.Data.Trend == nil {
		return summary, nil
	}

	requests := 0.0
	tokens := 0.0
	cost := 0.0
	daily := make(map[string]UsageTrendPoint)
	for _, item := range *payload.Data.Trend {
		requests += item.Requests
		tokens += item.TotalTokens
		cost += item.ActualCost
		date := normalizeTrendDate(item.Date)
		if date != "" {
			point := daily[date]
			point.Date = date
			point.Requests += item.Requests
			point.Tokens += item.TotalTokens
			point.Cost += item.ActualCost
			daily[date] = point
		}
	}
	summary.Requests = supportedMetric(requests, "request")
	summary.Tokens = supportedMetric(tokens, "token")
	summary.Cost = supportedMetric(cost, "usd")
	summary.Trend = fillDailyTrend(window, daily)
	return summary, nil
}

func sub2CurrentRPM(ctx context.Context, connector *Connector, credential *CredentialMaterial) MetricSample {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.UTC
	}
	now := time.Now().In(location)
	query := url.Values{
		"start_date":          {now.Format("2006-01-02")},
		"end_date":            {now.AddDate(0, 0, 1).Format("2006-01-02")},
		"timezone":            {location.String()},
		"granularity":         {"hour"},
		"include_stats":       {"true"},
		"include_trend":       {"false"},
		"include_model_stats": {"false"},
		"include_group_stats": {"false"},
	}
	endpoint := "/api/v1/admin/dashboard/snapshot-v2"
	if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		endpoint = "/api/v1/usage/dashboard/snapshot-v2"
		query.Del("include_stats")
	}
	response, requestErr := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if requestErr == nil && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		if value, ok := findRPMValue(response.Body); ok && value >= 0 {
			return supportedMetric(value, "request/min")
		}
	}
	if value, ok := sub2RPMFromRecentUsage(ctx, connector, credential); ok {
		return supportedMetric(value, "request/min")
	}
	return unsupportedMetric("request/min")
}

func findRPMValue(body []byte) (float64, bool) {
	var payload any
	if json.Unmarshal(body, &payload) != nil {
		return 0, false
	}
	return findNamedJSONNumber(payload, map[string]bool{
		"rpm": true, "current_rpm": true, "rpm_current": true,
		"requests_per_minute": true, "request_per_minute": true,
	})
}

func findNamedJSONNumber(value any, names map[string]bool) (float64, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, candidate := range typed {
			if names[strings.ToLower(strings.TrimSpace(key))] {
				if number, ok := jsonNumber(candidate); ok {
					return number, true
				}
			}
		}
		for _, candidate := range typed {
			if number, ok := findNamedJSONNumber(candidate, names); ok {
				return number, true
			}
		}
	case []any:
		for _, candidate := range typed {
			if number, ok := findNamedJSONNumber(candidate, names); ok {
				return number, true
			}
		}
	}
	return 0, false
}

func jsonNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func sub2RPMFromRecentUsage(ctx context.Context, connector *Connector, credential *CredentialMaterial) (float64, bool) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.UTC
	}
	now := time.Now()
	localNow := now.In(location)
	cutoff := now.Add(-time.Minute)
	const pageSize = 100
	count := 0.0
	parsedTimestamp := false

	for pageNumber := 1; pageNumber <= managedInstanceInventoryMaxPages; pageNumber++ {
		query := url.Values{
			"page":       {strconv.Itoa(pageNumber)},
			"page_size":  {strconv.Itoa(pageSize)},
			"start_date": {localNow.Format("2006-01-02")},
			"end_date":   {localNow.AddDate(0, 0, 1).Format("2006-01-02")},
			"timezone":   {location.String()},
			"sort_by":    {"created_at"},
			"sort_order": {"desc"},
		}
		endpoint := "/api/v1/admin/usage"
		if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
			endpoint = "/api/v1/usage"
		}
		response, requestErr := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, endpoint+"?"+query.Encode(), nil)
		if requestErr != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return 0, false
		}
		data, decodeErr := sub2EnvelopeData(response)
		if decodeErr != nil {
			return 0, false
		}
		var result struct {
			Items    []json.RawMessage `json:"items"`
			Total    int               `json:"total"`
			Pages    int               `json:"pages"`
			Page     int               `json:"page"`
			PageSize int               `json:"page_size"`
		}
		if json.Unmarshal(data, &result) != nil || result.Items == nil || result.Total < 0 {
			return 0, false
		}
		if len(result.Items) == 0 {
			return count, true
		}
		reachedOlder := false
		for _, item := range result.Items {
			createdAt, ok := sub2UsageCreatedAt(item, location)
			if !ok {
				continue
			}
			parsedTimestamp = true
			if createdAt.Before(cutoff) {
				reachedOlder = true
				break
			}
			if !createdAt.After(now.Add(5 * time.Second)) {
				count++
			}
		}
		if reachedOlder || len(result.Items) < pageSize || (result.Pages > 0 && pageNumber >= result.Pages) || pageNumber*pageSize >= result.Total {
			return count, parsedTimestamp
		}
	}
	return count, parsedTimestamp
}

func sub2UsageCreatedAt(raw json.RawMessage, location *time.Location) (time.Time, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return time.Time{}, false
	}
	for _, key := range []string{"created_at", "createdAt", "timestamp", "time"} {
		value := fields[key]
		if unix, ok := jsonInt64(value); ok {
			if unix > 1_000_000_000_000 {
				unix /= 1000
			}
			return time.Unix(unix, 0), unix > 0
		}
		var text string
		if len(value) == 0 || json.Unmarshal(value, &text) != nil {
			continue
		}
		text = strings.TrimSpace(text)
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed, true
			}
		}
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
			if parsed, err := time.ParseInLocation(layout, text, location); err == nil {
				return parsed, true
			}
		}
	}
	return time.Time{}, false
}

func normalizeTrendDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= len("2006-01-02") {
		value = value[:len("2006-01-02")]
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return ""
	}
	return value
}

func fillDailyTrend(window TimeWindow, values map[string]UsageTrendPoint) []UsageTrendPoint {
	return fillDailyTrendInLocation(window, values, time.UTC)
}

func fillDailyTrendInLocation(window TimeWindow, values map[string]UsageTrendPoint, location *time.Location) []UsageTrendPoint {
	if location == nil {
		location = time.UTC
	}
	start := time.Unix(window.Start, 0).In(location)
	end := time.Unix(window.End, 0).In(location)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, location)
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, location)

	trend := make([]UsageTrendPoint, 0, int(end.Sub(start)/(24*time.Hour))+1)
	for cursor := start; !cursor.After(end); cursor = cursor.AddDate(0, 0, 1) {
		date := cursor.Format("2006-01-02")
		point := values[date]
		point.Date = date
		trend = append(trend, point)
	}
	return trend
}

func (genericAdapter) Inventory(context.Context, *Connector, *CredentialMaterial, string, string) (*InventoryPage, error) {
	return nil, ErrUnsupportedCapability
}

func (genericAdapter) Summary(context.Context, *Connector, *CredentialMaterial, TimeWindow) (*SummaryResult, error) {
	return nil, ErrUnsupportedCapability
}

func CollectInventory(ctx context.Context, instanceID int64, resourceKind string, cursor string) (*ObservationView, error) {
	return collectInventory(ctx, instanceID, resourceKind, cursor, nil)
}

func CollectInventoryWithCommitGuard(ctx context.Context, instanceID int64, resourceKind string, cursor string, guard CommitGuard) (*ObservationView, error) {
	return collectInventory(ctx, instanceID, resourceKind, cursor, guard)
}

func collectInventory(ctx context.Context, instanceID int64, resourceKind string, cursor string, guard CommitGuard) (*ObservationView, error) {
	cursor = strings.TrimSpace(cursor)
	if len(cursor) > 512 {
		return nil, ErrInvalidInstance
	}
	instance, adapter, connector, credential, err := observationClient(instanceID)
	if err != nil {
		return nil, err
	}
	observedAt := common.GetTimestamp()
	inventoryAdapter := adapter
	collectSub2Usage := false
	if sub2Adapter, ok := adapter.(sub2APIAdapter); ok && cursor == "" && credentialAccessScope(credential) != model.ManagedInstanceAccessUser && normalizeResourceKind(resourceKind, "account") == "account" {
		inventoryAdapter = sub2InventoryWithoutUsageAdapter{sub2Adapter}
		collectSub2Usage = true
	}
	page, collectionErr := inventoryAdapter.Inventory(ctx, connector, credential, resourceKind, cursor)
	if cursor == "" && collectionErr == nil {
		page, collectionErr = collectCompleteInventory(ctx, inventoryAdapter, connector, credential, resourceKind, page)
	}
	if collectSub2Usage && collectionErr == nil {
		enrichSub2AccountUsage(ctx, connector, credential, page)
	}
	if cursor != "" {
		view, _, err := observationView(instance.Id, observedAt, page, collectionErr)
		return view, err
	}
	return persistObservationWithGuard(instance.Id, model.ManagedInstanceSnapshotTypeInventory, normalizeResourceKind(resourceKind, defaultResourceKind(instance.Kind)), observedAt, page, collectionErr, guard)
}

func collectCompleteInventory(ctx context.Context, adapter InstanceAdapter, connector *Connector, credential *CredentialMaterial, resourceKind string, first *InventoryPage) (*InventoryPage, error) {
	if first == nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	if supportsParallelInventory(adapter, first.ResourceKind) {
		return collectParallelInventory(ctx, adapter, connector, credential, resourceKind, first)
	}
	combined := &InventoryPage{
		ResourceKind: first.ResourceKind,
		Items:        append([]InventoryItem(nil), first.Items...),
		Total:        first.Total,
		NextCursor:   first.NextCursor,
	}
	seen := map[string]struct{}{}
	for pageNumber := 1; combined.NextCursor != ""; pageNumber++ {
		if pageNumber >= managedInstanceInventoryMaxPages || len(combined.Items) >= managedInstanceInventoryMaxItems {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
		}
		cursor := combined.NextCursor
		if _, duplicate := seen[cursor]; duplicate {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
		}
		seen[cursor] = struct{}{}
		page, err := adapter.Inventory(ctx, connector, credential, resourceKind, cursor)
		if err != nil {
			return nil, err
		}
		if page == nil || page.ResourceKind != combined.ResourceKind || len(combined.Items)+len(page.Items) > managedInstanceInventoryMaxItems {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
		}
		combined.Items = append(combined.Items, page.Items...)
		if page.Total > combined.Total {
			combined.Total = page.Total
		}
		combined.NextCursor = page.NextCursor
	}
	if combined.Total < len(combined.Items) {
		combined.Total = len(combined.Items)
	}
	if combined.Total > len(combined.Items) {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	return combined, nil
}

type sub2InventoryWithoutUsageAdapter struct{ sub2APIAdapter }

func (adapter sub2InventoryWithoutUsageAdapter) Inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind, cursor string) (*InventoryPage, error) {
	return adapter.inventory(ctx, connector, credential, resourceKind, cursor, false)
}

func supportsParallelInventory(adapter InstanceAdapter, resourceKind string) bool {
	if resourceKind != "account" {
		return false
	}
	return adapter.Kind() == model.ManagedInstanceKindSub2API || adapter.Kind() == model.ManagedInstanceKindConductor
}

type parallelInventoryPage struct {
	page *InventoryPage
	err  error
}

func collectParallelInventory(ctx context.Context, adapter InstanceAdapter, connector *Connector, credential *CredentialMaterial, resourceKind string, first *InventoryPage) (*InventoryPage, error) {
	combined := &InventoryPage{ResourceKind: first.ResourceKind, Items: make([]InventoryItem, 0, max(first.Total, len(first.Items)))}
	seenIDs := make(map[int64]struct{}, max(first.Total, len(first.Items)))
	maxReportedTotal := first.Total
	appendPage := func(page *InventoryPage) error {
		if page == nil || page.ResourceKind != combined.ResourceKind {
			return &ProbeError{Code: ProbeErrorInvalidResponse}
		}
		if page.Total > maxReportedTotal {
			maxReportedTotal = page.Total
		}
		for _, item := range page.Items {
			if item.ID <= 0 {
				continue
			}
			if _, duplicate := seenIDs[item.ID]; duplicate {
				continue
			}
			if len(combined.Items) >= managedInstanceInventoryMaxItems {
				return &ProbeError{Code: ProbeErrorInvalidResponse}
			}
			seenIDs[item.ID] = struct{}{}
			combined.Items = append(combined.Items, item)
		}
		return nil
	}
	if err := appendPage(first); err != nil {
		return nil, err
	}

	pageStride := parallelInventoryPageStride(first)
	knownLastPage := parallelInventoryKnownLastPage(first.Total, pageStride)
	exhausted := false
	speculative := false
	for firstPage := 2; firstPage <= managedInstanceInventoryMaxPages; {
		pageCount := 1
		if firstPage <= knownLastPage {
			pageCount = min(managedInstanceInventoryWorkers, knownLastPage-firstPage+1)
		} else if speculative {
			pageCount = 1
		}
		results := make([]parallelInventoryPage, pageCount)
		var workers sync.WaitGroup
		for index := range pageCount {
			pageNumber := firstPage + index
			cursor := parallelInventoryCursor(adapter.Kind(), resourceKind, pageNumber, pageStride)
			workers.Add(1)
			go func(resultIndex int) {
				defer workers.Done()
				results[resultIndex].page, results[resultIndex].err = adapter.Inventory(ctx, connector, credential, resourceKind, cursor)
			}(index)
		}
		workers.Wait()

		previousCount := len(combined.Items)
		var batchErr error
		emptyPageReached := false
		for _, result := range results {
			if result.err != nil {
				if terminalInventoryPageError(result.err) {
					emptyPageReached = true
					continue
				}
				batchErr = result.err
				continue
			}
			if result.page == nil || len(result.page.Items) == 0 {
				emptyPageReached = true
				continue
			}
			if emptyPageReached {
				return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
			}
			if err := appendPage(result.page); err != nil {
				return nil, err
			}
		}
		if len(combined.Items) == previousCount {
			if batchErr != nil && maxReportedTotal > len(combined.Items) {
				return nil, batchErr
			}
			exhausted = true
			break
		}
		if batchErr != nil {
			return nil, batchErr
		}
		wasSpeculative := speculative && firstPage > knownLastPage
		if emptyPageReached {
			exhausted = true
			break
		}
		firstPage += pageCount
		if estimated := parallelInventoryKnownLastPage(maxReportedTotal, pageStride); estimated > knownLastPage {
			knownLastPage = estimated
		}
		if firstPage > knownLastPage {
			speculative = true
		}
		if wasSpeculative {
			knownLastPage = min(managedInstanceInventoryMaxPages, firstPage+managedInstanceInventoryWorkers-1)
		}
	}
	if !exhausted || maxReportedTotal > len(combined.Items) {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	combined.Total = len(combined.Items)
	return combined, nil
}

func parallelInventoryPageStride(first *InventoryPage) int {
	if first != nil && len(first.Items) > 0 {
		return len(first.Items)
	}
	return 1
}

func parallelInventoryKnownLastPage(total, stride int) int {
	if total <= 0 || stride <= 0 {
		return 1
	}
	return max(1, (total+stride-1)/stride)
}

func parallelInventoryCursor(kind, resourceKind string, pageNumber, stride int) string {
	if kind == model.ManagedInstanceKindConductor {
		return fmt.Sprintf("conductor:%s:%d", resourceKind, (pageNumber-1)*stride)
	}
	return fmt.Sprintf("sub2api:%d", pageNumber)
}

func terminalInventoryPageError(err error) bool {
	var probeErr *ProbeError
	if !errors.As(err, &probeErr) {
		return false
	}
	switch probeErr.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestedRangeNotSatisfiable, http.StatusUnprocessableEntity:
		return true
	default:
		return false
	}
}

func newAPIPageNumber(cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 1, nil
	}
	const prefix = "newapi:"
	if !strings.HasPrefix(cursor, prefix) {
		return 0, ErrInvalidInstance
	}
	page, err := strconv.Atoi(strings.TrimPrefix(cursor, prefix))
	if err != nil || page < 2 || page > managedInstanceInventoryMaxPages {
		return 0, ErrInvalidInstance
	}
	return page, nil
}

func newAPIInventoryEndpoint(page int) string {
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(managedInstanceInventoryPageSize))
	return "/api/channel/?" + query.Encode()
}

func sub2InventoryEndpoint(cursor string) (string, int, error) {
	page, err := sub2PageNumber(cursor)
	if err != nil {
		return "", 0, err
	}
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("page_size", strconv.Itoa(managedInstanceInventoryPageSize))
	return "/api/v1/admin/accounts?" + query.Encode(), page, nil
}

func sub2PageNumber(cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 1, nil
	}
	const prefix = "sub2api:"
	if !strings.HasPrefix(cursor, prefix) {
		return 0, ErrInvalidInstance
	}
	page, err := strconv.Atoi(strings.TrimPrefix(cursor, prefix))
	if err != nil || page < 2 || page > managedInstanceInventoryMaxPages {
		return 0, ErrInvalidInstance
	}
	return page, nil
}

func sub2NextPageCursor(data json.RawMessage, requestedPage int) string {
	var page struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
		Pages    int `json:"pages"`
		Total    int `json:"total"`
	}
	if json.Unmarshal(data, &page) != nil {
		return ""
	}
	currentPage := page.Page
	if currentPage <= 0 {
		currentPage = requestedPage
	}
	totalPages := page.Pages
	if totalPages <= 0 && page.PageSize > 0 && page.Total > 0 {
		totalPages = (page.Total + page.PageSize - 1) / page.PageSize
	}
	if currentPage < totalPages {
		return fmt.Sprintf("sub2api:%d", currentPage+1)
	}
	return ""
}

func CollectSummary(ctx context.Context, instanceID int64, window TimeWindow) (*ObservationView, error) {
	return collectSummary(ctx, instanceID, window, nil)
}

func CollectRealtimeMetrics(ctx context.Context, instanceID int64) (*ObservationView, error) {
	instance, _, connector, credential, err := observationClient(instanceID)
	if err != nil {
		return nil, err
	}
	result := &RealtimeMetricsResult{RPM: unsupportedMetric("request/min")}
	switch instance.Kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan:
		result.RPM = newAPICurrentRPM(ctx, connector, instance.Kind, credential)
	case model.ManagedInstanceKindSub2API:
		result.RPM = sub2CurrentRPM(ctx, connector, credential)
	case model.ManagedInstanceKindConductor:
		result.RPM = conductorCurrentRPM(ctx, connector, credential)
	}
	view, _, err := observationView(instance.Id, common.GetTimestamp(), result, nil)
	return view, err
}

func CollectSummaryWithCommitGuard(ctx context.Context, instanceID int64, window TimeWindow, guard CommitGuard) (*ObservationView, error) {
	return collectSummary(ctx, instanceID, window, guard)
}

func collectSummary(ctx context.Context, instanceID int64, window TimeWindow, guard CommitGuard) (*ObservationView, error) {
	instance, adapter, connector, credential, err := observationClient(instanceID)
	if err != nil {
		return nil, err
	}
	if window.End == 0 {
		window.End = common.GetTimestamp()
	}
	if window.Start == 0 {
		window.Start = window.End - 86400
	}
	if window.Start < 0 || window.Start >= window.End {
		return nil, ErrInvalidInstance
	}
	observedAt := common.GetTimestamp()
	summary, collectionErr := adapter.Summary(ctx, connector, credential, window)
	return persistObservationWithGuard(instance.Id, model.ManagedInstanceSnapshotTypeSummary, "", observedAt, summary, collectionErr, guard)
}

func observationClient(instanceID int64) (*model.ManagedInstance, InstanceAdapter, *Connector, *CredentialMaterial, error) {
	if instanceID <= 0 {
		return nil, nil, nil, nil, ErrInvalidInstance
	}
	var instance model.ManagedInstance
	if err := model.DB.First(&instance, instanceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, nil, ErrInstanceNotFound
		}
		return nil, nil, nil, nil, err
	}
	credential, err := loadCredential(instanceID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	policy, err := ConnectorPolicyFromEnvironment()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	connector, err := NewConnector(&instance, policy)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	adapter, err := adapterForKind(instance.Kind)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return &instance, adapter, connector, credential, nil
}

func persistObservation(instanceID int64, snapshotType string, resourceKind string, observedAt int64, data any, collectionErr error) (*ObservationView, error) {
	return persistObservationWithGuard(instanceID, snapshotType, resourceKind, observedAt, data, collectionErr, nil)
}

func persistObservationWithGuard(instanceID int64, snapshotType string, resourceKind string, observedAt int64, data any, collectionErr error, guard CommitGuard) (*ObservationView, error) {
	view, payload, err := observationView(instanceID, observedAt, data, collectionErr)
	if err != nil {
		return nil, err
	}
	snapshot := &model.ManagedInstanceSnapshot{
		InstanceId: instanceID, SnapshotType: snapshotType, ResourceKind: resourceKind,
		SchemaVersion: managedInstanceSnapshotSchemaVersion, ObservedAt: observedAt,
		ETag: view.ETag, Payload: string(payload), CollectionStatus: view.CollectionStatus, ErrorCode: view.ErrorCode,
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if guard != nil {
			if err := guard(tx); err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "instance_id"}, {Name: "snapshot_type"}, {Name: "resource_kind"}},
			DoUpdates: clause.AssignmentColumns([]string{"schema_version", "observed_at", "etag", "payload", "collection_status", "error_code", "updated_at"}),
		}).Create(snapshot).Error
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}

func observationView(instanceID int64, observedAt int64, data any, collectionErr error) (*ObservationView, []byte, error) {
	status := model.ManagedInstanceCollectionSucceeded
	errorCode := ""
	if collectionErr != nil {
		status = model.ManagedInstanceCollectionFailed
		errorCode = managedInstanceObservationErrorCode(collectionErr)
		if errors.Is(collectionErr, ErrUnsupportedCapability) {
			status = model.ManagedInstanceCollectionUnsupported
		}
		data = nil
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, nil, err
	}
	digest := sha256.Sum256(payload)
	view := &ObservationView{
		SourceInstanceID: instanceID, ObservedAt: observedAt, CollectionStatus: status,
		ErrorCode: errorCode, ETag: hex.EncodeToString(digest[:]), Data: data,
	}
	return view, payload, nil
}

func newAPIEnvelopeData(response *ConnectorResponse) (json.RawMessage, error) {
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || !envelope.Success || len(envelope.Data) == 0 {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return envelope.Data, nil
}

func sub2EnvelopeData(response *ConnectorResponse) (json.RawMessage, error) {
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	var envelope struct {
		Code any             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || !sub2SuccessCode(envelope.Code) || len(envelope.Data) == 0 {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return envelope.Data, nil
}

func normalizeInventoryPage(resourceKind string, data json.RawMessage) (*InventoryPage, error) {
	items, total, nextCursor, err := extractInventoryRows(data)
	if err != nil {
		return nil, err
	}
	normalized := make([]InventoryItem, 0, len(items))
	for _, item := range items {
		row, ok := normalizeInventoryItem(item)
		if ok {
			normalized = append(normalized, row)
		}
	}
	if total < len(normalized) {
		total = len(normalized)
	}
	return &InventoryPage{ResourceKind: resourceKind, Items: normalized, Total: total, NextCursor: nextCursor}, nil
}

func extractInventoryRows(data json.RawMessage) ([]json.RawMessage, int, string, error) {
	var rows []json.RawMessage
	if err := json.Unmarshal(data, &rows); err == nil {
		return rows, len(rows), "", nil
	}
	var page struct {
		Items      []json.RawMessage `json:"items"`
		Data       []json.RawMessage `json:"data"`
		Total      *int              `json:"total"`
		NextCursor string            `json:"next_cursor"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, 0, "", &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	rows = page.Items
	if rows == nil {
		rows = page.Data
	}
	if rows == nil {
		return nil, 0, "", &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	total := len(rows)
	if page.Total != nil && *page.Total >= 0 {
		total = *page.Total
	}
	return rows, total, page.NextCursor, nil
}

func normalizeInventoryItem(raw json.RawMessage) (InventoryItem, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return InventoryItem{}, false
	}
	id, ok := jsonInt64(fields["id"])
	if !ok || id <= 0 {
		return InventoryItem{}, false
	}
	status := firstJSONText(fields, "status", "state")
	createdAt, _ := firstJSONUnixTime(fields, "created_at", "created_time", "uploaded_at", "upload_time")
	lastActivityAt, _ := firstJSONUnixTime(fields, "last_used_at", "test_time")
	responseTime, hasResponseTime := firstJSONInt64(fields, "response_time")
	usedQuota, hasUsedQuota := firstJSONFloat64(fields, "used_quota")
	balance, hasBalance := firstJSONFloat64(fields, "balance")
	var responseTimeValue *int64
	if hasResponseTime {
		responseTimeValue = &responseTime
	}
	var costValue *float64
	costUnit := ""
	if hasUsedQuota {
		costValue = &usedQuota
		costUnit = "quota"
	}
	var balanceValue *float64
	if hasBalance {
		balanceValue = &balance
	}
	return InventoryItem{
		ID: id, Name: firstJSONText(fields, "name", "username", "email", "label"),
		Type: firstJSONText(fields, "type", "provider"), Platform: firstJSONText(fields, "platform"),
		Group: firstJSONText(fields, "group", "group_name"), Status: status, Enabled: normalizedEnabled(fields, status),
		CreatedAt: createdAt, LastActivityAt: lastActivityAt, Cost: costValue, CostUnit: costUnit,
		Balance: balanceValue, ResponseTimeMS: responseTimeValue,
		ErrorMessage: firstJSONText(fields, "error_message", "error"),
	}, true
}

type sub2AccountUsage struct {
	Requests float64
	Tokens   float64
	Cost     float64
}

func enrichSub2AccountUsage(ctx context.Context, connector *Connector, credential *CredentialMaterial, page *InventoryPage) {
	if page == nil || len(page.Items) == 0 {
		return
	}

	type usageResult struct {
		Index int
		Usage *sub2AccountUsage
	}
	const workerLimit = 12
	workerCount := min(workerLimit, len(page.Items))
	jobs := make(chan int)
	results := make(chan usageResult, len(page.Items))
	var workers sync.WaitGroup
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				usage := fetchSub2AccountLifetimeUsage(ctx, connector, credential, page.Items[index])
				results <- usageResult{Index: index, Usage: usage}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range page.Items {
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	for result := range results {
		applySub2AccountUsage(&page.Items[result.Index], result.Usage)
	}
}

func fetchSub2AccountLifetimeUsage(ctx context.Context, connector *Connector, credential *CredentialMaterial, item InventoryItem) *sub2AccountUsage {
	if item.ID <= 0 {
		return nil
	}
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.UTC
	}
	now := time.Now().In(location)
	if item.CreatedAt > 0 {
		created := time.Unix(item.CreatedAt, 0).In(location)
		days := int(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).Sub(
			time.Date(created.Year(), created.Month(), created.Day(), 0, 0, 0, 0, location),
		).Hours()/24) + 1
		if days < 1 {
			days = 1
		}
		if days <= 90 {
			if usage := fetchSub2AccountStats(ctx, connector, credential, item.ID, days); usage != nil {
				return usage
			}
		}
	}
	return fetchSub2AccountUsageRecords(ctx, connector, credential, item.ID, item.CreatedAt, now, location)
}

func fetchSub2AccountStats(ctx context.Context, connector *Connector, credential *CredentialMaterial, accountID int64, days int) *sub2AccountUsage {
	query := url.Values{"days": {strconv.Itoa(days)}}
	endpoint := "/api/v1/admin/accounts/" + strconv.FormatInt(accountID, 10) + "/stats?" + query.Encode()
	response, err := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, endpoint, nil)
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil
	}
	data, err := sub2EnvelopeData(response)
	if err != nil {
		return nil
	}
	var payload struct {
		Summary *struct {
			Requests float64 `json:"total_requests"`
			Tokens   float64 `json:"total_tokens"`
			Cost     float64 `json:"total_cost"`
		} `json:"summary"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Summary == nil {
		return nil
	}
	return &sub2AccountUsage{Requests: payload.Summary.Requests, Tokens: payload.Summary.Tokens, Cost: payload.Summary.Cost}
}

func fetchSub2AccountUsageRecords(ctx context.Context, connector *Connector, credential *CredentialMaterial, accountID int64, createdAt int64, now time.Time, location *time.Location) *sub2AccountUsage {
	start := time.Unix(0, 0).In(location)
	if createdAt > 0 {
		start = time.Unix(createdAt, 0).In(location)
	}
	total := &sub2AccountUsage{}
	for pageNumber := 1; pageNumber <= managedInstanceInventoryMaxPages; pageNumber++ {
		query := url.Values{
			"account_id": {strconv.FormatInt(accountID, 10)},
			"start_date": {start.Format("2006-01-02")},
			"end_date":   {now.AddDate(0, 0, 1).Format("2006-01-02")},
			"timezone":   {location.String()},
			"page":       {strconv.Itoa(pageNumber)},
			"page_size":  {strconv.Itoa(usageRecordMaxPageSize)},
		}
		response, err := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/admin/usage?"+query.Encode(), nil)
		if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return nil
		}
		page, err := decodeUsageRecordPage(model.ManagedInstanceKindSub2API, response.Body)
		if err != nil {
			return nil
		}
		if len(page.Items) == 0 {
			return total
		}
		for _, raw := range page.Items {
			tokens, cost, err := sub2AccountUsageRecordTotals(raw)
			if err != nil {
				return nil
			}
			total.Requests++
			total.Tokens += tokens
			total.Cost += cost
		}
		if len(page.Items) < page.PageSize {
			return total
		}
	}
	return nil
}

func sub2AccountUsageRecordTotals(raw json.RawMessage) (float64, float64, error) {
	var item map[string]any
	if json.Unmarshal(raw, &item) != nil {
		return 0, 0, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	value := func(key string) (float64, bool) {
		return usageNumber(item[key])
	}
	number := func(key string) float64 {
		result, _ := value(key)
		return result
	}
	tokens := number("input_tokens") + number("output_tokens") + number("cache_read_tokens") + number("cache_creation_tokens")
	baseCost, hasBaseCost := value("account_stats_cost")
	if !hasBaseCost {
		baseCost, hasBaseCost = value("total_cost")
	}
	if hasBaseCost {
		multiplier := 1.0
		if configured, ok := value("account_rate_multiplier"); ok {
			multiplier = configured
		}
		return tokens, baseCost * multiplier, nil
	}
	return tokens, number("actual_cost"), nil
}

func applySub2AccountUsage(item *InventoryItem, usage *sub2AccountUsage) {
	if item == nil || usage == nil {
		return
	}
	item.Requests = &usage.Requests
	item.Tokens = &usage.Tokens
	item.Cost = &usage.Cost
	item.CostUnit = "usd"
}

func summaryFromInventory(window TimeWindow, page *InventoryPage) *SummaryResult {
	enabled := 0
	unhealthy := 0
	enabledKnown := page.Total <= len(page.Items)
	unhealthyKnown := page.Total <= len(page.Items)
	for _, item := range page.Items {
		if item.Enabled == nil {
			enabledKnown = false
		} else if *item.Enabled {
			enabled++
		}
		state := strings.ToLower(item.Status)
		if strings.Contains(state, "error") || strings.Contains(state, "fail") || strings.Contains(state, "invalid") || strings.Contains(state, "expired") || strings.Contains(state, "limit") {
			unhealthy++
		}
	}
	var enabledValue *int
	if enabledKnown {
		enabledValue = &enabled
	}
	var unhealthyValue *int
	if unhealthyKnown {
		unhealthyValue = &unhealthy
	}
	unsupported := func(unit string) MetricSample {
		return MetricSample{Unit: unit, CollectionStatus: model.ManagedInstanceCollectionUnsupported}
	}
	return &SummaryResult{
		Window:    window,
		Resources: []ResourceSummary{{ResourceKind: page.ResourceKind, Total: page.Total, Enabled: enabledValue, Unhealthy: unhealthyValue}},
		Requests:  unsupported("request"), Tokens: unsupported("token"), Cost: unsupported("remote_currency"),
		ErrorRate: unsupported("ratio"), Latency: unsupported("ms"),
	}
}

func defaultResourceKind(instanceKind string) string {
	if instanceKind == model.ManagedInstanceKindSub2API || instanceKind == model.ManagedInstanceKindConductor {
		return "account"
	}
	return "channel"
}

func normalizeResourceKind(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "auto":
		return fallback
	case "channels":
		return "channel"
	case "accounts":
		return "account"
	default:
		return value
	}
}

func firstJSONText(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		var value string
		if raw := fields[name]; len(raw) > 0 && json.Unmarshal(raw, &value) == nil {
			return strings.TrimSpace(value)
		}
		var number json.Number
		if raw := fields[name]; len(raw) > 0 && json.Unmarshal(raw, &number) == nil {
			return number.String()
		}
	}
	return ""
}

func firstJSONInt64(fields map[string]json.RawMessage, names ...string) (int64, bool) {
	for _, name := range names {
		if value, ok := jsonInt64(fields[name]); ok {
			return value, true
		}
	}
	return 0, false
}

func firstJSONFloat64(fields map[string]json.RawMessage, names ...string) (float64, bool) {
	for _, name := range names {
		var value float64
		if raw := fields[name]; len(raw) > 0 && json.Unmarshal(raw, &value) == nil {
			return value, true
		}
		var text string
		if raw := fields[name]; len(raw) > 0 && json.Unmarshal(raw, &text) == nil {
			value, err := strconv.ParseFloat(text, 64)
			if err == nil {
				return value, true
			}
		}
	}
	return 0, false
}

func firstJSONUnixTime(fields map[string]json.RawMessage, names ...string) (int64, bool) {
	for _, name := range names {
		if value, ok := jsonInt64(fields[name]); ok {
			return value, true
		}
		var text string
		if raw := fields[name]; len(raw) > 0 && json.Unmarshal(raw, &text) == nil {
			parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(text))
			if err == nil {
				return parsed.Unix(), true
			}
		}
	}
	return 0, false
}

func jsonInt64(raw json.RawMessage) (int64, bool) {
	var number int64
	if len(raw) > 0 && json.Unmarshal(raw, &number) == nil {
		return number, true
	}
	var text string
	if len(raw) > 0 && json.Unmarshal(raw, &text) == nil {
		value, err := strconv.ParseInt(text, 10, 64)
		return value, err == nil
	}
	return 0, false
}

func normalizedEnabled(fields map[string]json.RawMessage, status string) *bool {
	var result *bool
	var explicitEnabled bool
	if raw := fields["enabled"]; len(raw) > 0 && json.Unmarshal(raw, &explicitEnabled) == nil {
		result = &explicitEnabled
	}
	var numeric int
	if result == nil {
		if raw := fields["status"]; len(raw) > 0 && json.Unmarshal(raw, &numeric) == nil {
			if numeric >= 1 && numeric <= 3 {
				value := numeric == 1
				result = &value
			}
		}
	}
	if result == nil {
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "active", "enabled", "healthy", "ok", "valid":
			value := true
			result = &value
		case "inactive", "disabled", "offline", "invalid", "expired":
			value := false
			result = &value
		}
	}
	var schedulable bool
	if raw := fields["schedulable"]; len(raw) > 0 && json.Unmarshal(raw, &schedulable) == nil {
		if result == nil {
			return &schedulable
		}
		value := *result && schedulable
		return &value
	}
	return result
}

func managedInstanceObservationErrorCode(err error) string {
	var probeError *ProbeError
	if errors.As(err, &probeError) {
		return probeError.Code
	}
	var tlsVerificationError *tls.CertificateVerificationError
	var unknownAuthorityError x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var certificateInvalidError x509.CertificateInvalidError
	var tlsRecordError tls.RecordHeaderError
	var dnsError *net.DNSError
	var networkError net.Error
	switch {
	case errors.As(err, &tlsVerificationError), errors.As(err, &unknownAuthorityError), errors.As(err, &hostnameError), errors.As(err, &certificateInvalidError):
		return "tls_verification_failed"
	case errors.As(err, &tlsRecordError):
		return "tls_failed"
	case errors.As(err, &dnsError):
		return "dns_failed"
	case errors.Is(err, ErrUnsupportedCapability):
		return "unsupported_capability"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "collection_cancelled"
	case errors.Is(err, ErrConnectorTargetBlocked):
		return "target_blocked"
	case errors.Is(err, ErrConnectorResponseLarge):
		return "response_too_large"
	case errors.As(err, &networkError):
		return "network_failed"
	default:
		return "collection_failed"
	}
}
