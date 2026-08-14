package managedinstance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	case "api_key":
		return conductorKeyInventory(ctx, connector, credential, cursor)
	default:
		return nil, ErrUnsupportedCapability
	}
}

type conductorAccount struct {
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
	RPMCurrent             *int    `json:"rpm_current"`
	Utilization5H          float64 `json:"utilization_5h"`
	Utilization7D          float64 `json:"utilization_7d"`
	Utilization7DOI        float64 `json:"utilization_7d_oi"`
	Removed                bool    `json:"_removed"`
	Cause                  struct {
		StatusNote any `json:"status_note"`
	} `json:"cause"`
}

func decodeConductorAccounts(decoder *json.Decoder) ([]conductorAccount, error) {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	accounts := make([]conductorAccount, 0, managedInstanceInventoryPageSize)
	for decoder.More() {
		if len(accounts) >= managedInstanceInventoryMaxItems {
			return nil, ErrUsageExportTooLarge
		}
		var account conductorAccount
		if err = decoder.Decode(&account); err != nil {
			return nil, err
		}
		for _, value := range []string{
			account.AccountID, account.Source, account.Email, account.Label, account.AuthType, account.SubscriptionType,
			account.Status, account.Health, account.BlockedReason, account.UnavailableKind, account.DispatchState,
		} {
			if len(value) > usageRecordMaxTextValue {
				return nil, ErrConnectorResponseLarge
			}
		}
		accounts = append(accounts, account)
	}
	if _, err = decoder.Token(); err != nil {
		return nil, err
	}
	return accounts, nil
}

func conductorAccountItem(account conductorAccount) (InventoryItem, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(account.AccountID), 10, 64)
	if err != nil || id <= 0 {
		return InventoryItem{}, false
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
	activeSessions := account.ActiveSessionCount
	u5, u7, u7oi := account.Utilization5H, account.Utilization7D, account.Utilization7DOI
	return InventoryItem{
		ID: id, Name: name, Type: account.AuthType, Platform: account.Source, SourceID: strings.TrimSpace(account.Source), Group: account.SubscriptionType,
		Status: status, Enabled: &enabled, CreatedAt: account.CreatedAt, LastActivityAt: account.DispatchStateChangedAt,
		ActiveSessions: &activeSessions, RPM: account.RPMCurrent, Utilization5H: &u5, Utilization7D: &u7, Utilization7DOI: &u7oi,
		ErrorMessage: errorMessage,
	}, true
}

func conductorAccountInventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, cursor string) (*InventoryPage, error) {
	offset, err := conductorOffset(cursor, "account")
	if err != nil {
		return nil, err
	}
	if connector != nil && connector.instanceID > 0 {
		if state, ok := activeConductorRealtime(connector.instanceID); ok {
			items := state.Accounts
			if offset > len(items) {
				offset = len(items)
			}
			end := min(offset+managedInstanceInventoryPageSize, len(items))
			page := conductorInventoryPage("account", items[offset:end], len(items), offset)
			page.Sources = append([]InventorySource(nil), state.Sources...)
			return page, nil
		}
	}
	query := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(managedInstanceInventoryPageSize)}}
	response, err := conductorOpenJSONResponse(ctx, connector, credential, "/api/v1/accounts?"+query.Encode())
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, probeHTTPError(response.StatusCode)
	}
	accounts, total, err := decodeConductorAccountInventory(response.Body, response.StatusCode)
	if err != nil {
		return nil, err
	}
	items := make([]InventoryItem, 0, len(accounts))
	for _, account := range accounts {
		if item, ok := conductorAccountItem(account); ok {
			items = append(items, item)
		}
	}
	return conductorInventoryPage("account", items, total, offset), nil
}

func conductorOpenJSONResponse(ctx context.Context, connector *Connector, credential *CredentialMaterial, path string) (*ConnectorStream, error) {
	headers, err := conductorAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.OpenResponse(ctx, http.MethodGet, path, headers, "application/json")
	if err != nil || credential.AuthType != "account_password" || response == nil || (response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden) {
		return response, err
	}
	response.Body.Close()
	invalidateConductorSession(connector, credential, strings.TrimPrefix(headers.Get("Authorization"), "Bearer "))
	headers, err = conductorAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	return connector.OpenResponse(ctx, http.MethodGet, path, headers, "application/json")
}

func decodeConductorAccountInventory(reader io.Reader, statusCode int) ([]conductorAccount, int, error) {
	decoder := json.NewDecoder(reader)
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: statusCode}
	}
	code := 0
	for decoder.More() {
		key, keyErr := conductorJSONKey(decoder)
		if keyErr != nil {
			return nil, 0, keyErr
		}
		switch key {
		case "code":
			err = decoder.Decode(&code)
		case "data":
			accounts, total, dataErr := decodeConductorAccountInventoryData(decoder)
			if dataErr != nil {
				return nil, 0, dataErr
			}
			if code == http.StatusUnauthorized || code == http.StatusForbidden {
				return nil, 0, probeHTTPError(code)
			}
			if code != 0 && code != http.StatusOK && code != http.StatusCreated {
				return nil, 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: statusCode}
			}
			return accounts, total, nil
		default:
			err = discardConductorJSONValue(decoder)
		}
		if err != nil {
			return nil, 0, err
		}
	}
	return nil, 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: statusCode}
}

func decodeConductorAccountInventoryData(decoder *json.Decoder) ([]conductorAccount, int, error) {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, 0, &ProbeError{Code: ProbeErrorInvalidResponse}
	}
	var accounts []conductorAccount
	total := -1
	for decoder.More() {
		key, keyErr := conductorJSONKey(decoder)
		if keyErr != nil {
			return nil, 0, keyErr
		}
		switch key {
		case "accounts":
			accounts, err = decodeConductorAccounts(decoder)
		case "total":
			err = decoder.Decode(&total)
		default:
			err = discardConductorJSONValue(decoder)
		}
		if err != nil {
			return nil, 0, err
		}
		if accounts != nil && total >= 0 {
			if len(accounts) > managedInstanceInventoryMaxItems {
				return nil, 0, ErrUsageExportTooLarge
			}
			return accounts, total, nil
		}
	}
	return nil, 0, &ProbeError{Code: ProbeErrorInvalidResponse}
}

func conductorKeyInventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, cursor string) (*InventoryPage, error) {
	if strings.TrimSpace(cursor) != "" {
		return nil, ErrInvalidInstance
	}
	usersResponse, err := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/users", nil)
	if err != nil {
		return nil, err
	}
	usersData, err := conductorEnvelopeData(usersResponse)
	if err != nil {
		return nil, err
	}
	var users []struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if json.Unmarshal(usersData, &users) != nil || users == nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: usersResponse.StatusCode}
	}
	items := make([]InventoryItem, 0)
	for _, user := range users {
		if user.ID <= 0 {
			continue
		}
		query := url.Values{"user_id": {strconv.FormatInt(user.ID, 10)}}
		response, requestErr := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/keys?"+query.Encode(), nil)
		if requestErr != nil {
			return nil, requestErr
		}
		data, decodeErr := conductorEnvelopeData(response)
		if decodeErr != nil {
			return nil, decodeErr
		}
		var keys []struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			IsActive  bool   `json:"is_active"`
			ExpiresAt string `json:"expires_at"`
			CreatedAt string `json:"created_at"`
			Settings  struct {
				SelectionStrategy string `json:"selection_strategy"`
			} `json:"settings"`
		}
		if json.Unmarshal(data, &keys) != nil || keys == nil {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
		}
		for _, key := range keys {
			if key.ID <= 0 {
				continue
			}
			enabled := key.IsActive
			status := "disabled"
			if enabled {
				status = "active"
			}
			items = append(items, InventoryItem{
				ID: key.ID, Name: key.Name, Type: key.Settings.SelectionStrategy, Group: user.Username,
				Status: status, Enabled: &enabled, CreatedAt: parseConductorTime(key.CreatedAt),
			})
			if len(items) >= managedInstanceInventoryMaxItems {
				return nil, ErrUsageExportTooLarge
			}
		}
	}
	return &InventoryPage{ResourceKind: "api_key", Items: items, Total: len(items)}, nil
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
	sources, err := conductorInventorySources(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	items := make([]InventoryItem, 0, len(sources))
	for _, source := range sources {
		id, parseErr := strconv.ParseInt(source.ID, 10, 64)
		if parseErr != nil || id <= 0 {
			continue
		}
		items = append(items, InventoryItem{ID: id, Name: source.Name, Type: source.URL, Status: source.Status, Enabled: source.Enabled, AccountCount: source.AccountCount})
	}
	return &InventoryPage{ResourceKind: "ws_client", Items: items, Sources: sources, Total: len(items)}, nil
}

func conductorInventorySources(ctx context.Context, connector *Connector, credential *CredentialMaterial) ([]InventorySource, error) {
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
	sources := make([]InventorySource, 0, len(clients))
	for _, client := range clients {
		if client.ID <= 0 {
			continue
		}
		enabled, count := client.Enabled, client.AccountCount
		sources = append(sources, InventorySource{ID: strconv.FormatInt(client.ID, 10), Name: client.Name, URL: client.URL, Status: client.Health, Enabled: &enabled, AccountCount: &count})
	}
	return sources, nil
}

func enrichConductorInventorySources(ctx context.Context, connector *Connector, credential *CredentialMaterial, page *InventoryPage) {
	if page == nil || page.ResourceKind != "account" {
		return
	}
	sources, err := conductorInventorySources(ctx, connector, credential)
	if err != nil {
		return
	}
	page.Sources = sources
	byID := make(map[string]InventorySource, len(sources))
	for _, source := range sources {
		byID[source.ID] = source
	}
	for index := range page.Items {
		source, ok := byID[page.Items[index].SourceID]
		if ok && strings.TrimSpace(source.Name) != "" {
			page.Items[index].Platform = source.Name
		}
	}
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
	if err != nil || offset <= 0 || offset >= managedInstanceInventoryMaxItems {
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

func conductorRPMCapacityPerAccount(ctx context.Context, connector *Connector, credential *CredentialMaterial) (float64, error) {
	response, err := conductorDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/system/quota", nil)
	if err != nil {
		return 0, err
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return 0, err
	}
	capacity, ok := conductorRPMCapacityFromData(data)
	if !ok {
		return 0, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return capacity, nil
}

func conductorRPMCapacityFromData(data []byte) (float64, bool) {
	var quota struct {
		PerAccount struct {
			Capacity float64 `json:"min_interval_ms"`
		} `json:"per_account"`
	}
	if json.Unmarshal(data, &quota) != nil || quota.PerAccount.Capacity <= 0 {
		return 0, false
	}
	return quota.PerAccount.Capacity, true
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
