package managedinstance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
)

const mercerRouterSystemName = "MercerRouter"

type mercerRouterAdapter struct{}

func (mercerRouterAdapter) Kind() string { return model.ManagedInstanceKindMercerRouter }

func isMercerRouterSystem(systemName string) bool {
	return strings.EqualFold(strings.TrimSpace(systemName), mercerRouterSystemName)
}

func (adapter mercerRouterAdapter) Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error) {
	response, err := connector.DoJSON(ctx, http.MethodGet, "/api/status", nil, nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, probeHTTPError(response.StatusCode)
	}
	var status newAPIStatusResponse
	if err := json.Unmarshal(response.Body, &status); err != nil || !status.Success || !isMercerRouterSystem(status.Data.SystemName) {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return adapter.probeWithStatus(ctx, connector, credential, status)
}

func (mercerRouterAdapter) probeWithStatus(ctx context.Context, connector *Connector, credential *CredentialMaterial, status newAPIStatusResponse) (*ProbeResult, error) {
	if credential == nil || credential.AuthType != "account_password" || strings.TrimSpace(credential.UserID) == "" || strings.TrimSpace(credential.Secret) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	response, err := newAPIDoJSON(ctx, connector, model.ManagedInstanceKindMercerRouter, credential, http.MethodGet, "/api/user/self", nil)
	if err != nil {
		return nil, err
	}
	data, err := newAPIEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	userID, hasUserID := firstJSONInt64(fields, "id", "user_id")
	role, hasRole := firstJSONInt64(fields, "role")
	if !hasUserID || userID <= 0 || !hasRole {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	accessScope := ""
	switch role {
	case 5:
		accessScope = model.ManagedInstanceAccessChannelAdmin
	case 10, 100:
		accessScope = model.ManagedInstanceAccessAdmin
	default:
		return nil, &ProbeError{Code: ProbeErrorPermission, StatusCode: http.StatusForbidden}
	}
	return &ProbeResult{
		Kind: model.ManagedInstanceKindMercerRouter, AccessScope: accessScope,
		Version: status.Data.Version, SystemName: status.Data.SystemName, StartTime: status.Data.StartTime,
		Status: model.ManagedInstanceStatusHealthy,
		Capabilities: []string{
			"health.read", "version.read", "profile.read", "channels.list", "dashboard.read", "usage.read",
		},
	}, nil
}

func (mercerRouterAdapter) Inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind string, cursor string) (*InventoryPage, error) {
	resourceKind = normalizeResourceKind(resourceKind, "channel")
	if resourceKind != "channel" {
		return nil, ErrUnsupportedCapability
	}
	pageNumber, err := mercerRouterPageNumber(cursor)
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"tag_mode":  {"false"},
		"id_sort":   {"true"},
		"p":         {strconv.Itoa(pageNumber)},
		"page_size": {strconv.Itoa(managedInstanceInventoryPageSize)},
	}
	response, err := newAPIDoJSON(ctx, connector, model.ManagedInstanceKindMercerRouter, credential, http.MethodGet, "/api/channel/?"+query.Encode(), nil)
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
		page.NextCursor = fmt.Sprintf("mercer:%d", pageNumber+1)
	}
	return page, nil
}

func mercerRouterPageNumber(cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 1, nil
	}
	const prefix = "mercer:"
	if !strings.HasPrefix(cursor, prefix) {
		return 0, ErrInvalidInstance
	}
	page, err := strconv.Atoi(strings.TrimPrefix(cursor, prefix))
	if err != nil || page < 2 || page > managedInstanceInventoryMaxPages {
		return 0, ErrInvalidInstance
	}
	return page, nil
}

func (mercerRouterAdapter) Summary(ctx context.Context, connector *Connector, credential *CredentialMaterial, window TimeWindow) (*SummaryResult, error) {
	location, timezone := summaryLocation(window.Timezone)
	window.Timezone = timezone
	query := url.Values{
		"start_timestamp": {strconv.FormatInt(window.Start, 10)},
		"end_timestamp":   {strconv.FormatInt(window.End, 10)},
		"default_time":    {mercerRouterGranularity(window)},
	}
	endpoint := "/api/data"
	if credentialAccessScope(credential) == model.ManagedInstanceAccessChannelAdmin {
		endpoint = "/api/data/self"
	}
	response, err := newAPIDoJSON(ctx, connector, model.ManagedInstanceKindMercerRouter, credential, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	data, err := newAPIEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}

	requests := 0.0
	tokens := 0.0
	daily := make(map[string]UsageTrendPoint)
	cost := 0.0
	costAvailable := !mercerRouterCostHidden(response.Body)
	needsQuotaConversion := false
	for _, row := range rows {
		count, _ := firstJSONFloat64(row, "count", "request_count", "requests")
		tokenCount, _ := firstJSONFloat64(row, "token_used", "tokens", "total_tokens")
		requests += count
		tokens += tokenCount
		rowCost, hasRowCost := firstJSONFloat64(row, "amount_usd")
		if !hasRowCost {
			needsQuotaConversion = true
		}
		date := mercerRouterRowDate(row, location)
		point := daily[date]
		point.Date = date
		point.Requests += count
		point.Tokens += tokenCount
		if hasRowCost {
			cost += rowCost
			point.Cost += rowCost
		}
		if date != "" {
			daily[date] = point
		}
	}

	quotaPerUnit := 0.0
	if costAvailable && needsQuotaConversion {
		quotaPerUnit, costAvailable = mercerRouterQuotaPerUnit(ctx, connector, credential)
	}
	if costAvailable && needsQuotaConversion {
		for _, row := range rows {
			if _, hasUSD := firstJSONFloat64(row, "amount_usd"); hasUSD {
				continue
			}
			quota, hasQuota := firstJSONFloat64(row, "quota")
			if !hasQuota {
				costAvailable = false
				break
			}
			value := quota / quotaPerUnit
			cost += value
			date := mercerRouterRowDate(row, location)
			if date != "" {
				point := daily[date]
				point.Cost += value
				daily[date] = point
			}
		}
	}

	summary := &SummaryResult{
		Window:   window,
		Requests: supportedMetric(requests, "request"), Tokens: supportedMetric(tokens, "token"),
		Cost: unsupportedMetric("usd"), ErrorRate: unsupportedMetric("ratio"), Latency: unsupportedMetric("ms"),
		Trend: fillDailyTrendInLocation(window, daily, location),
	}
	if costAvailable {
		summary.Cost = supportedMetric(cost, "usd")
	}
	return summary, nil
}

func mercerRouterGranularity(window TimeWindow) string {
	duration := time.Duration(max(int64(0), window.End-window.Start)) * time.Second
	switch {
	case duration <= 7*24*time.Hour:
		return "hour"
	case duration <= 31*24*time.Hour:
		return "day"
	default:
		return "week"
	}
}

func mercerRouterRowDate(row map[string]json.RawMessage, location *time.Location) string {
	if value, ok := firstJSONUnixTime(row, "created_at", "timestamp", "time"); ok && value > 0 {
		if value > 1_000_000_000_000 {
			value /= 1000
		}
		return time.Unix(value, 0).In(location).Format("2006-01-02")
	}
	text := firstJSONText(row, "created_at", "date", "time")
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.In(location).Format("2006-01-02")
		}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, text, location); err == nil {
			return parsed.Format("2006-01-02")
		}
	}
	return ""
}

func mercerRouterQuotaPerUnit(ctx context.Context, connector *Connector, credential *CredentialMaterial) (float64, bool) {
	response, err := newAPIDoJSON(ctx, connector, model.ManagedInstanceKindMercerRouter, credential, http.MethodGet, "/api/status", nil)
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || mercerRouterCostHidden(response.Body) {
		return 0, false
	}
	var payload any
	if json.Unmarshal(response.Body, &payload) != nil {
		return 0, false
	}
	value, ok := findNamedJSONNumber(payload, map[string]bool{"quota_per_unit": true})
	return value, ok && value > 0
}

func mercerRouterCostHidden(body []byte) bool {
	var payload any
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	return findMercerRouterCostHidden(payload)
}

func findMercerRouterCostHidden(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, candidate := range typed {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "redacted":
				if flag, ok := candidate.(bool); ok && flag {
					return true
				}
			case "cost_visible":
				if flag, ok := candidate.(bool); ok && !flag {
					return true
				}
			}
		}
		for _, candidate := range typed {
			if findMercerRouterCostHidden(candidate) {
				return true
			}
		}
	case []any:
		for _, candidate := range typed {
			if findMercerRouterCostHidden(candidate) {
				return true
			}
		}
	}
	return false
}

func mercerRouterCurrentRPM(ctx context.Context, connector *Connector, credential *CredentialMaterial) (MetricSample, error) {
	now := time.Now()
	query := url.Values{
		"p":               {"1"},
		"page_size":       {"1"},
		"start_timestamp": {strconv.FormatInt(now.Add(-time.Minute).Unix(), 10)},
		"end_timestamp":   {strconv.FormatInt(now.Unix(), 10)},
	}
	endpoint := "/api/log/stat"
	if credentialAccessScope(credential) == model.ManagedInstanceAccessChannelAdmin {
		endpoint = "/api/maas/channel-admin/usage-logs/stat"
	}
	response, err := newAPIDoJSON(ctx, connector, model.ManagedInstanceKindMercerRouter, credential, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return unsupportedMetric("request/min"), err
	}
	data, err := newAPIEnvelopeData(response)
	if err != nil {
		return unsupportedMetric("request/min"), err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return unsupportedMetric("request/min"), &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	rpm, ok := firstJSONFloat64(fields, "rpm", "count", "total_count")
	if !ok || rpm < 0 {
		return unsupportedMetric("request/min"), &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return supportedMetric(rpm, "request/min"), nil
}
