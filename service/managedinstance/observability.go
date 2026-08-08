package managedinstance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const managedInstanceSnapshotSchemaVersion = 1

const (
	managedInstanceInventoryPageSize = 100
	managedInstanceInventoryMaxItems = 10000
	managedInstanceInventoryMaxPages = 100
)

type TimeWindow struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type InventoryItem struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type,omitempty"`
	Group   string `json:"group,omitempty"`
	Status  string `json:"status,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
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

type SummaryResult struct {
	Window    TimeWindow        `json:"window"`
	Resources []ResourceSummary `json:"resources"`
	Requests  MetricSample      `json:"requests"`
	Tokens    MetricSample      `json:"tokens"`
	Cost      MetricSample      `json:"cost"`
	ErrorRate MetricSample      `json:"error_rate"`
	Latency   MetricSample      `json:"latency"`
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
	resourceKind = normalizeResourceKind(resourceKind, "channel")
	if resourceKind != "channel" {
		return nil, ErrUnsupportedCapability
	}
	headers, err := newAPIAuthHeaders(adapter.configuredKind, credential)
	if err != nil {
		return nil, err
	}
	pageNumber, err := newAPIPageNumber(cursor)
	if err != nil {
		return nil, err
	}
	response, err := connector.DoJSON(ctx, http.MethodGet, newAPIInventoryEndpoint(pageNumber), headers, nil)
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
	return summaryFromInventory(window, page), nil
}

func (adapter sub2APIAdapter) Inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind string, cursor string) (*InventoryPage, error) {
	resourceKind = normalizeResourceKind(resourceKind, "account")
	if resourceKind != "account" {
		return nil, ErrUnsupportedCapability
	}
	headers, err := sub2APIAuthHeaders(credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.DoJSON(ctx, http.MethodGet, inventoryEndpoint("/api/v1/admin/accounts", cursor), headers, nil)
	if err != nil {
		return nil, err
	}
	data, err := sub2EnvelopeData(response)
	if err != nil {
		return nil, err
	}
	return normalizeInventoryPage("account", data)
}

func (adapter sub2APIAdapter) Summary(ctx context.Context, connector *Connector, credential *CredentialMaterial, window TimeWindow) (*SummaryResult, error) {
	page, err := adapter.Inventory(ctx, connector, credential, "account", "")
	if err != nil {
		return nil, err
	}
	return summaryFromInventory(window, page), nil
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
	page, collectionErr := adapter.Inventory(ctx, connector, credential, resourceKind, cursor)
	if cursor == "" && collectionErr == nil {
		page, collectionErr = collectCompleteInventory(ctx, adapter, connector, credential, resourceKind, page)
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

func inventoryEndpoint(path string, cursor string) string {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return path
	}
	query := url.Values{}
	query.Set("cursor", cursor)
	return path + "?" + query.Encode()
}

func CollectSummary(ctx context.Context, instanceID int64, window TimeWindow) (*ObservationView, error) {
	return collectSummary(ctx, instanceID, window, nil)
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
	status := firstJSONString(fields, "status", "state")
	return InventoryItem{
		ID: id, Name: firstJSONString(fields, "name", "username", "email", "label"),
		Type: firstJSONString(fields, "type", "platform", "provider"), Group: firstJSONString(fields, "group", "group_name"),
		Status: status, Enabled: normalizedEnabled(fields, status),
	}, true
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
	if instanceKind == model.ManagedInstanceKindSub2API {
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

func firstJSONString(fields map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		var value string
		if raw := fields[name]; len(raw) > 0 && json.Unmarshal(raw, &value) == nil {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
	var enabled bool
	if raw := fields["enabled"]; len(raw) > 0 && json.Unmarshal(raw, &enabled) == nil {
		return &enabled
	}
	var numeric int
	if raw := fields["status"]; len(raw) > 0 && json.Unmarshal(raw, &numeric) == nil {
		if numeric == 1 || numeric == 2 {
			value := numeric == 1
			return &value
		}
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "enabled", "healthy", "ok", "valid":
		value := true
		return &value
	case "inactive", "disabled", "offline", "invalid", "expired":
		value := false
		return &value
	default:
		return nil
	}
}

func managedInstanceObservationErrorCode(err error) string {
	var probeError *ProbeError
	if errors.As(err, &probeError) {
		return probeError.Code
	}
	switch {
	case errors.Is(err, ErrUnsupportedCapability):
		return "unsupported_capability"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "collection_cancelled"
	case errors.Is(err, ErrConnectorTargetBlocked):
		return "target_blocked"
	case errors.Is(err, ErrConnectorResponseLarge):
		return "response_too_large"
	default:
		return "collection_failed"
	}
}
