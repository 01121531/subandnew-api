package managedinstance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
)

type conductorAdapter struct{}

type conductorEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type conductorSession struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

const conductorSessionTTL = 30 * time.Minute

var conductorSessions sync.Map

func (conductorAdapter) Kind() string { return model.ManagedInstanceKindConductor }

func (conductorAdapter) Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error) {
	response, err := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/system/health", nil)
	if err != nil {
		return nil, err
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var health struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(data, &health) != nil || !strings.EqualFold(strings.TrimSpace(health.Status), "ok") {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return &ProbeResult{
		Kind: model.ManagedInstanceKindConductor, SystemName: "Conductor",
		Status: model.ManagedInstanceStatusHealthy,
		Capabilities: []string{
			"health.read", "quota.read", "stats.read", "accounts.list", "users.list",
			"ws_clients.list", "prices.list", "usage.read",
		},
	}, nil
}

func conductorAuthHeaders(ctx context.Context, connector *Connector, credential *CredentialMaterial) (http.Header, error) {
	if credential == nil || strings.TrimSpace(credential.Secret) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	if credential.AuthType == "account_password" {
		return loginConductor(ctx, connector, credential)
	}
	if credential.AuthType != "bearer_pat" && credential.AuthType != "admin_token" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	return conductorBearerHeaders(strings.TrimSpace(credential.Secret)), nil
}

func loginConductor(ctx context.Context, connector *Connector, credential *CredentialMaterial) (http.Header, error) {
	username := strings.TrimSpace(credential.UserID)
	if username == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	key := credentialSessionKey(connector, credential)
	value, _ := conductorSessions.LoadOrStore(key, &conductorSession{})
	state := value.(*conductorSession)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.token != "" && time.Now().Before(state.expiresAt) {
		return conductorBearerHeaders(state.token), nil
	}
	response, err := connector.DoJSON(ctx, http.MethodPost, "/api/v1/auth/login", nil, map[string]string{
		"username": username, "password": credential.Secret,
	})
	if err != nil {
		return nil, err
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var login struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(data, &login) != nil || strings.TrimSpace(login.Token) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication, StatusCode: response.StatusCode}
	}
	state.token = strings.TrimSpace(login.Token)
	state.expiresAt = time.Now().Add(conductorSessionTTL)
	return conductorBearerHeaders(state.token), nil
}

func conductorBearerHeaders(token string) http.Header {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	return headers
}

func invalidateConductorSession(connector *Connector, credential *CredentialMaterial, token string) {
	value, ok := conductorSessions.Load(credentialSessionKey(connector, credential))
	if !ok {
		return
	}
	state := value.(*conductorSession)
	state.mu.Lock()
	defer state.mu.Unlock()
	if token == "" || state.token == token {
		state.token = ""
		state.expiresAt = time.Time{}
	}
}

func conductorDoJSON(ctx context.Context, connector *Connector, credential *CredentialMaterial, method, path string, body any) (*ConnectorResponse, error) {
	headers, err := conductorAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.DoJSON(ctx, method, path, headers, body)
	if err != nil || credential.AuthType != "account_password" || !authenticationRejected(response) {
		return response, err
	}
	invalidateConductorSession(connector, credential, strings.TrimPrefix(headers.Get("Authorization"), "Bearer "))
	headers, err = conductorAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	return connector.DoJSON(ctx, method, path, headers, body)
}

func conductorEnvelopeData(response *ConnectorResponse) (json.RawMessage, error) {
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	var envelope conductorEnvelope
	if json.Unmarshal(response.Body, &envelope) != nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	if envelope.Code == http.StatusUnauthorized || envelope.Code == http.StatusForbidden {
		return nil, probeHTTPError(envelope.Code)
	}
	if (envelope.Code != 0 && envelope.Code != http.StatusOK && envelope.Code != http.StatusCreated) || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return envelope.Data, nil
}

func (adapter conductorAdapter) Inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind, cursor string) (*InventoryPage, error) {
	resourceKind = normalizeResourceKind(resourceKind, "account")
	switch resourceKind {
	case "account":
		return conductorAccountInventory(ctx, connector, credential, cursor)
	case "user":
		return conductorUserInventory(ctx, connector, credential, cursor)
	case "ws_client":
		return conductorWSClientInventory(ctx, connector, credential, cursor)
	case "price":
		return conductorPriceInventory(ctx, connector, credential, cursor)
	default:
		return nil, ErrUnsupportedCapability
	}
}

func conductorAccountInventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, cursor string) (*InventoryPage, error) {
	offset, err := conductorOffset(cursor, "account")
	if err != nil {
		return nil, err
	}
	query := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(managedInstanceInventoryPageSize)}}
	response, err := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/accounts?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Accounts []struct {
			AccountID              string  `json:"account_id"`
			Source                 string  `json:"source"`
			Email                  string  `json:"email"`
			Label                  string  `json:"label"`
			AuthType               string  `json:"auth_type"`
			SubscriptionType       string  `json:"subscription_type"`
			Status                 string  `json:"status"`
			Health                 string  `json:"health"`
			Available              bool    `json:"available"`
			Blocked                bool    `json:"blocked"`
			BlockedReason          string  `json:"blocked_reason"`
			UnavailableKind        string  `json:"unavailable_kind"`
			DispatchState          string  `json:"dispatch_state"`
			DispatchStateChangedAt int64   `json:"dispatch_state_changed_at"`
			CreatedAt              int64   `json:"created_at"`
			ActiveSessionCount     int     `json:"active_session_count"`
			RPMCurrent             int     `json:"rpm_current"`
			Utilization5H          float64 `json:"utilization_5h"`
			Utilization7D          float64 `json:"utilization_7d"`
			Utilization7DOI        float64 `json:"utilization_7d_oi"`
			Cause                  struct {
				StatusNote any `json:"status_note"`
			} `json:"cause"`
		} `json:"accounts"`
		Total int `json:"total"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Accounts == nil || payload.Total < 0 {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	items := make([]InventoryItem, 0, len(payload.Accounts))
	for _, account := range payload.Accounts {
		id, parseErr := strconv.ParseInt(account.AccountID, 10, 64)
		if parseErr != nil || id <= 0 {
			continue
		}
		name := strings.TrimSpace(account.Label)
		if name == "" {
			name = strings.TrimSpace(account.Email)
		}
		status := firstNonEmpty(account.Health, account.Status, account.DispatchState)
		enabled := account.Available && !account.Blocked
		errorMessage := firstNonEmpty(account.BlockedReason, account.UnavailableKind)
		if errorMessage == "" && account.Cause.StatusNote != nil {
			errorMessage = strings.TrimSpace(fmt.Sprint(account.Cause.StatusNote))
		}
		activeSessions, rpm := account.ActiveSessionCount, account.RPMCurrent
		u5, u7, u7oi := account.Utilization5H, account.Utilization7D, account.Utilization7DOI
		items = append(items, InventoryItem{
			ID: id, Name: name, Type: account.AuthType, Platform: account.Source, Group: account.SubscriptionType,
			Status: status, Enabled: &enabled, CreatedAt: account.CreatedAt, LastActivityAt: account.DispatchStateChangedAt,
			ActiveSessions: &activeSessions, RPM: &rpm, Utilization5H: &u5, Utilization7D: &u7, Utilization7DOI: &u7oi,
			ErrorMessage: errorMessage,
		})
	}
	return conductorInventoryPage("account", items, payload.Total, offset), nil
}

func conductorUserInventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, cursor string) (*InventoryPage, error) {
	if strings.TrimSpace(cursor) != "" {
		return nil, ErrInvalidInstance
	}
	response, err := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/users", nil)
	if err != nil {
		return nil, err
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var users []struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		Role      string `json:"role"`
		IsActive  bool   `json:"is_active"`
		CreatedAt string `json:"created_at"`
	}
	if json.Unmarshal(data, &users) != nil || users == nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	items := make([]InventoryItem, 0, len(users))
	for _, user := range users {
		if user.ID <= 0 {
			continue
		}
		active := user.IsActive
		status := "disabled"
		if active {
			status = "active"
		}
		items = append(items, InventoryItem{ID: user.ID, Name: user.Username, Type: user.Role, Status: status, Enabled: &active, CreatedAt: parseConductorTime(user.CreatedAt)})
	}
	return &InventoryPage{ResourceKind: "user", Items: items, Total: len(items)}, nil
}

func conductorWSClientInventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, cursor string) (*InventoryPage, error) {
	if strings.TrimSpace(cursor) != "" {
		return nil, ErrInvalidInstance
	}
	response, err := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/ws-clients", nil)
	if err != nil {
		return nil, err
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var clients []struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		URL          string `json:"url"`
		Enabled      bool   `json:"enabled"`
		Health       string `json:"health"`
		AccountCount int    `json:"account_count"`
	}
	if json.Unmarshal(data, &clients) != nil || clients == nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	items := make([]InventoryItem, 0, len(clients))
	for _, client := range clients {
		if client.ID <= 0 {
			continue
		}
		enabled, count := client.Enabled, client.AccountCount
		items = append(items, InventoryItem{ID: client.ID, Name: client.Name, Type: client.URL, Status: client.Health, Enabled: &enabled, AccountCount: &count})
	}
	return &InventoryPage{ResourceKind: "ws_client", Items: items, Total: len(items)}, nil
}

func conductorPriceInventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, cursor string) (*InventoryPage, error) {
	if strings.TrimSpace(cursor) != "" {
		return nil, ErrInvalidInstance
	}
	prices, err := conductorPrices(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	items := make([]InventoryItem, 0, len(prices))
	for _, price := range prices {
		input, output, read, create := price.InputPricePerM, price.OutputPricePerM, price.CacheReadPricePerM, price.CacheCreatePricePerM
		items = append(items, InventoryItem{ID: price.ID, Name: price.Model, Type: "model_price", Status: price.Note,
			InputPricePerM: &input, OutputPricePerM: &output, CacheReadPricePerM: &read, CacheCreatePricePerM: &create})
	}
	return &InventoryPage{ResourceKind: "price", Items: items, Total: len(items)}, nil
}

func conductorInventoryPage(kind string, items []InventoryItem, total, offset int) *InventoryPage {
	next := ""
	if offset+len(items) < total {
		next = fmt.Sprintf("conductor:%s:%d", kind, offset+len(items))
	}
	return &InventoryPage{ResourceKind: kind, Items: items, Total: total, NextCursor: next}
}

func conductorOffset(cursor, kind string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	prefix := "conductor:" + kind + ":"
	if !strings.HasPrefix(cursor, prefix) {
		return 0, ErrInvalidInstance
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(cursor, prefix))
	if err != nil || offset < managedInstanceInventoryPageSize || offset >= managedInstanceInventoryMaxItems {
		return 0, ErrInvalidInstance
	}
	return offset, nil
}

func (adapter conductorAdapter) Summary(ctx context.Context, connector *Connector, credential *CredentialMaterial, window TimeWindow) (*SummaryResult, error) {
	response, err := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/system/health", nil)
	if err != nil {
		return nil, err
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var health struct {
		AccountsTotal     int `json:"accounts_total"`
		AccountsAvailable int `json:"accounts_available"`
		AccountsPaused    int `json:"accounts_paused"`
		AccountsRejected  int `json:"accounts_rejected"`
	}
	if json.Unmarshal(data, &health) != nil || health.AccountsTotal < 0 {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	enabled, unhealthy := health.AccountsAvailable, health.AccountsPaused+health.AccountsRejected
	unsupported := func(unit string) MetricSample {
		return MetricSample{Unit: unit, CollectionStatus: model.ManagedInstanceCollectionUnsupported}
	}
	result := &SummaryResult{
		Window: window, Resources: []ResourceSummary{{ResourceKind: "account", Total: health.AccountsTotal, Enabled: &enabled, Unhealthy: &unhealthy}},
		ErrorRate: unsupported("ratio"), Latency: unsupported("ms"),
	}
	aggregate, aggregateErr := conductorUsageAggregateForWindow(ctx, connector, credential, window)
	if aggregateErr != nil {
		result.Requests, result.Tokens, result.Cost = unsupported("request"), unsupported("token"), unsupported("USD")
		if recorded, ok := conductorRecordedUsage(ctx, connector, credential); ok {
			result.Requests = supportedMetric(recorded, "request")
		}
		return result, nil
	}
	result.Requests = supportedMetric(aggregate.Requests, "request")
	result.Tokens = supportedMetric(aggregate.Tokens, "token")
	result.Cost = supportedMetric(aggregate.Cost, "USD")
	location, locationErr := time.LoadLocation("Asia/Shanghai")
	if locationErr != nil {
		location = time.UTC
	}
	daily := make(map[string]UsageTrendPoint, len(aggregate.Trend))
	for _, point := range aggregate.Trend {
		daily[point.Date] = point
	}
	result.Trend = fillDailyTrendInLocation(window, daily, location)
	return result, nil
}

func conductorCurrentRPM(ctx context.Context, connector *Connector, credential *CredentialMaterial) MetricSample {
	cursor := ""
	total := 0.0
	found := false
	for pageNumber := 0; pageNumber < managedInstanceInventoryMaxPages; pageNumber++ {
		page, err := conductorAccountInventory(ctx, connector, credential, cursor)
		if err != nil {
			return unsupportedMetric("request/min")
		}
		for _, item := range page.Items {
			if item.RPM == nil || *item.RPM < 0 {
				continue
			}
			found = true
			total += float64(*item.RPM)
		}
		if page.NextCursor == "" {
			if !found && page.Total == 0 {
				return supportedMetric(0, "request/min")
			}
			if found {
				return supportedMetric(total, "request/min")
			}
			return unsupportedMetric("request/min")
		}
		cursor = page.NextCursor
	}
	return unsupportedMetric("request/min")
}

func conductorRecordedUsage(ctx context.Context, connector *Connector, credential *CredentialMaterial) (float64, bool) {
	response, err := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/system/stats", nil)
	if err != nil {
		return 0, false
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return 0, false
	}
	var stats struct {
		Usage struct {
			Recorded float64 `json:"recorded"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &stats) != nil {
		return 0, false
	}
	return stats.Usage.Recorded, true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func parseConductorTime(value string) int64 {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Unix()
		}
	}
	return 0
}
