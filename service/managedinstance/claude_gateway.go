package managedinstance

import (
	"bytes"
	"context"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
)

type claudeGatewayAdapter struct{}

type claudeGatewaySession struct {
	mu          sync.Mutex
	accessToken string
	cookies     []*http.Cookie
	expiresAt   time.Time
}

const claudeGatewaySessionTTL = 10 * time.Minute

var claudeGatewaySessions sync.Map

type claudeGatewayNumber float64

func (value *claudeGatewayNumber) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) || bytes.Equal(data, []byte(`""`)) {
		*value = 0
		return nil
	}
	var number json.Number
	if data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		number = json.Number(strings.TrimSpace(text))
	} else {
		number = json.Number(string(data))
	}
	parsed, err := number.Float64()
	if err != nil {
		return err
	}
	*value = claudeGatewayNumber(parsed)
	return nil
}

type claudeGatewayAccount struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Email            string              `json:"email"`
	AccountType      string              `json:"account_type"`
	AuthKind         string              `json:"auth_kind"`
	Status           string              `json:"status"`
	HealthStatus     string              `json:"health_status"`
	FailureKind      string              `json:"failure_kind"`
	LastError        string              `json:"last_error"`
	Provider         string              `json:"provider"`
	InferenceBackend string              `json:"inference_backend"`
	GroupName        string              `json:"group_name"`
	CreatedAt        string              `json:"created_at"`
	LastUsedAt       string              `json:"last_used_at"`
	TotalRequests    claudeGatewayNumber `json:"total_requests"`
	TotalTokens      claudeGatewayNumber `json:"total_tokens"`
	TotalCost        claudeGatewayNumber `json:"total_cost"`
	UsageWindows     struct {
		Requests7D claudeGatewayNumber `json:"req_7d"`
		Tokens7D   claudeGatewayNumber `json:"tokens_7d"`
		Cost7D     claudeGatewayNumber `json:"cost_7d"`
	} `json:"usage_windows"`
	Stats struct {
		RPM               int                 `json:"rpm"`
		Concurrent        int                 `json:"concurrent"`
		ActiveSessions    int                 `json:"active_sessions"`
		DailyRequests     claudeGatewayNumber `json:"daily_req"`
		DailyTokens       claudeGatewayNumber `json:"daily_tok"`
		DailyCost         claudeGatewayNumber `json:"daily_cost"`
		Cooldown          bool                `json:"cooldown"`
		CooldownReason    string              `json:"cooldown_reason"`
		CooldownRemaining int                 `json:"cooldown_remaining_seconds"`
	} `json:"stats"`
}

type claudeGatewayTodaySummary struct {
	TotalCost    claudeGatewayNumber `json:"total_cost"`
	TotalCost7D  claudeGatewayNumber `json:"total_cost_7d"`
	RequestCount claudeGatewayNumber `json:"request_count"`
}

type claudeGatewayOverview struct {
	KPIs struct {
		Total       claudeGatewayNumber `json:"total"`
		SuccessRate claudeGatewayNumber `json:"successRate"`
	} `json:"kpis"`
}

type claudeGatewayUsageSummary struct {
	TotalKeys     claudeGatewayNumber `json:"total_keys"`
	TotalRequests claudeGatewayNumber `json:"total_requests"`
	TotalTokens   claudeGatewayNumber `json:"total_tokens"`
	TotalCost     claudeGatewayNumber `json:"total_cost"`
}

func (claudeGatewayAdapter) Kind() string { return model.ManagedInstanceKindClaudeGateway }

func (claudeGatewayAdapter) Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error) {
	healthResponse, err := connector.DoJSON(ctx, http.MethodGet, "/api/health", nil, nil)
	if err != nil {
		return nil, err
	}
	if err := requireHTTPStatus(healthResponse); err != nil {
		return nil, err
	}
	var health struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	if json.Unmarshal(healthResponse.Body, &health) != nil || !strings.EqualFold(strings.TrimSpace(health.Status), "ok") || !strings.EqualFold(strings.TrimSpace(health.Service), "2coding-gateway-api") {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: healthResponse.StatusCode}
	}
	result := &ProbeResult{
		Kind: model.ManagedInstanceKindClaudeGateway, SystemName: "Claude Gateway",
		Status: model.ManagedInstanceStatusHealthy, Capabilities: []string{"health.read"},
	}
	if credential == nil {
		return result, nil
	}
	profile, err := claudeGatewayDoJSON(ctx, connector, credential, http.MethodGet, "/api/auth/me", nil)
	if err != nil {
		return nil, err
	}
	if err := requireHTTPStatus(profile); err != nil {
		return nil, err
	}
	if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		result.Capabilities = append(result.Capabilities, "profile.read")
		return result, nil
	}
	accounts, err := claudeGatewayDoJSON(ctx, connector, credential, http.MethodGet, "/api/admin/oauth-accounts/today-summary", nil)
	if err != nil {
		return nil, err
	}
	if err := requireHTTPStatus(accounts); err != nil {
		return nil, err
	}
	result.Capabilities = append(result.Capabilities, "profile.read", "accounts.list", "accounts.realtime", "usage.today.read")
	if systemInfo, systemErr := claudeGatewayDoJSON(ctx, connector, credential, http.MethodGet, "/api/admin/system/info", nil); systemErr == nil && systemInfo.StatusCode == http.StatusOK {
		var info struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(systemInfo.Body, &info) == nil {
			result.Version = strings.TrimSpace(info.Version)
			result.Capabilities = append(result.Capabilities, "version.read")
		}
	}
	return result, nil
}

func claudeGatewayAuthHeaders(ctx context.Context, connector *Connector, credential *CredentialMaterial) (http.Header, error) {
	if credential == nil || strings.TrimSpace(credential.Secret) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	if credential.AuthType == "account_password" {
		return loginClaudeGateway(ctx, connector, credential)
	}
	if credential.AuthType != "bearer_pat" && credential.AuthType != "admin_token" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	return claudeGatewayBearerHeaders(strings.TrimSpace(credential.Secret)), nil
}

func loginClaudeGateway(ctx context.Context, connector *Connector, credential *CredentialMaterial) (http.Header, error) {
	username := strings.TrimSpace(credential.UserID)
	if username == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	key := credentialSessionKey(connector, credential)
	value, _ := claudeGatewaySessions.LoadOrStore(key, &claudeGatewaySession{})
	state := value.(*claudeGatewaySession)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.accessToken != "" && time.Now().Before(state.expiresAt) {
		applyNewAPISession(connector, state.cookies)
		return claudeGatewayBearerHeaders(state.accessToken), nil
	}
	if len(state.cookies) > 0 {
		applyNewAPISession(connector, state.cookies)
		if response, err := connector.DoJSON(ctx, http.MethodPost, "/api/auth/refresh", nil, nil); err == nil && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
			if token := claudeGatewayAccessToken(response.Body); token != "" {
				state.accessToken = token
				state.cookies = cloneHTTPCookies(connector.client.Jar.Cookies(connector.baseURL))
				state.expiresAt = time.Now().Add(claudeGatewaySessionTTL)
				return claudeGatewayBearerHeaders(token), nil
			}
		}
	}
	_ = connector.resetCookies()
	response, err := connector.DoJSON(ctx, http.MethodPost, "/api/auth/admin-login", nil, map[string]string{
		"identifier": username, "email": username, "password": credential.Secret,
	})
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, probeHTTPError(response.StatusCode)
	}
	token := claudeGatewayAccessToken(response.Body)
	if token == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication, StatusCode: response.StatusCode}
	}
	state.accessToken = token
	state.cookies = cloneHTTPCookies(connector.client.Jar.Cookies(connector.baseURL))
	state.expiresAt = time.Now().Add(claudeGatewaySessionTTL)
	return claudeGatewayBearerHeaders(token), nil
}

func claudeGatewayAccessToken(body []byte) string {
	var response struct {
		AccessToken string `json:"accessToken"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ""
	}
	return strings.TrimSpace(response.AccessToken)
}

func claudeGatewayBearerHeaders(token string) http.Header {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	return headers
}

func invalidateClaudeGatewaySession(connector *Connector, credential *CredentialMaterial, token string) {
	if connector == nil || credential == nil {
		return
	}
	_ = connector.resetCookies()
	value, ok := claudeGatewaySessions.Load(credentialSessionKey(connector, credential))
	if !ok {
		return
	}
	state := value.(*claudeGatewaySession)
	state.mu.Lock()
	defer state.mu.Unlock()
	if token == "" || state.accessToken == token {
		state.accessToken = ""
		state.cookies = nil
		state.expiresAt = time.Time{}
	}
}

func claudeGatewayDoJSON(ctx context.Context, connector *Connector, credential *CredentialMaterial, method, path string, body any) (*ConnectorResponse, error) {
	headers, err := claudeGatewayAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.DoJSON(ctx, method, path, headers, body)
	if err != nil || credential.AuthType != "account_password" || !authenticationRejected(response) {
		return response, err
	}
	invalidateClaudeGatewaySession(connector, credential, strings.TrimPrefix(headers.Get("Authorization"), "Bearer "))
	headers, err = claudeGatewayAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	return connector.DoJSON(ctx, method, path, headers, body)
}

func (claudeGatewayAdapter) Inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind, cursor string) (*InventoryPage, error) {
	if strings.TrimSpace(cursor) != "" {
		return nil, ErrInvalidInstance
	}
	resourceKind = normalizeResourceKind(resourceKind, "account")
	if resourceKind != "account" || credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		return nil, ErrUnsupportedCapability
	}
	accounts, err := fetchClaudeGatewayAccounts(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	return claudeGatewayInventoryPage(accounts), nil
}

func fetchClaudeGatewayAccounts(ctx context.Context, connector *Connector, credential *CredentialMaterial) ([]claudeGatewayAccount, error) {
	response, err := claudeGatewayDoJSON(ctx, connector, credential, http.MethodGet, "/api/admin/oauth-accounts", nil)
	if err != nil {
		return nil, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	var envelope struct {
		Accounts []claudeGatewayAccount `json:"accounts"`
	}
	if json.Unmarshal(response.Body, &envelope) != nil || envelope.Accounts == nil || len(envelope.Accounts) > managedInstanceInventoryMaxItems {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return envelope.Accounts, nil
}

func fetchClaudeGatewayTodaySummary(ctx context.Context, connector *Connector, credential *CredentialMaterial) (claudeGatewayTodaySummary, error) {
	response, err := claudeGatewayDoJSON(ctx, connector, credential, http.MethodGet, "/api/admin/oauth-accounts/today-summary", nil)
	if err != nil {
		return claudeGatewayTodaySummary{}, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return claudeGatewayTodaySummary{}, err
	}
	var summary claudeGatewayTodaySummary
	if json.Unmarshal(response.Body, &summary) != nil {
		return claudeGatewayTodaySummary{}, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return summary, nil
}

func fetchClaudeGatewayOverview(ctx context.Context, connector *Connector, credential *CredentialMaterial) (claudeGatewayOverview, error) {
	response, err := claudeGatewayDoJSON(ctx, connector, credential, http.MethodGet, "/api/admin/overview?slice=time&granularity=day", nil)
	if err != nil {
		return claudeGatewayOverview{}, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return claudeGatewayOverview{}, err
	}
	var overview claudeGatewayOverview
	if json.Unmarshal(response.Body, &overview) != nil {
		return claudeGatewayOverview{}, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	total, successRate := float64(overview.KPIs.Total), float64(overview.KPIs.SuccessRate)
	if total < 0 || successRate < 0 || successRate > 1 {
		return claudeGatewayOverview{}, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return overview, nil
}

func fetchClaudeGatewayUsageSummary(ctx context.Context, connector *Connector, credential *CredentialMaterial, window TimeWindow) (claudeGatewayUsageSummary, error) {
	location, _ := summaryLocation(window.Timezone)
	query := url.Values{}
	query.Set("range", "custom")
	query.Set("from", time.Unix(window.Start, 0).In(location).Format("2006-01-02"))
	query.Set("to", time.Unix(window.End, 0).In(location).Format("2006-01-02"))
	query.Set("page", "1")
	query.Set("limit", "1")
	query.Set("sort", "cost")
	query.Set("direction", "desc")
	response, err := claudeGatewayDoJSON(ctx, connector, credential, http.MethodGet, "/api/admin/usage/keys?"+query.Encode(), nil)
	if err != nil {
		return claudeGatewayUsageSummary{}, err
	}
	if err := requireHTTPStatus(response); err != nil {
		return claudeGatewayUsageSummary{}, err
	}
	var envelope struct {
		Summary claudeGatewayUsageSummary `json:"summary"`
	}
	if json.Unmarshal(response.Body, &envelope) != nil {
		return claudeGatewayUsageSummary{}, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	requests, tokens, cost := float64(envelope.Summary.TotalRequests), float64(envelope.Summary.TotalTokens), float64(envelope.Summary.TotalCost)
	if requests < 0 || tokens < 0 || cost < 0 {
		return claudeGatewayUsageSummary{}, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return envelope.Summary, nil
}

func claudeGatewayInventoryPage(accounts []claudeGatewayAccount) *InventoryPage {
	items := make([]InventoryItem, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, claudeGatewayAccountItem(account))
	}
	return &InventoryPage{ResourceKind: "account", Items: items, Total: len(items)}
}

func claudeGatewayAccountItem(account claudeGatewayAccount) InventoryItem {
	name := firstNonEmpty(account.Name, account.Email, account.ID)
	status := firstNonEmpty(account.HealthStatus, account.Status)
	enabled := strings.EqualFold(strings.TrimSpace(account.Status), "active") && !strings.EqualFold(strings.TrimSpace(account.HealthStatus), "failed")
	requests, tokens, cost := float64(account.TotalRequests), float64(account.TotalTokens), float64(account.TotalCost)
	rpm, sessions := account.Stats.RPM, account.Stats.ActiveSessions
	return InventoryItem{
		ID: claudeGatewayStableID(account.ID), Name: name, Type: firstNonEmpty(account.AccountType, account.AuthKind),
		Platform: firstNonEmpty(account.Provider, account.InferenceBackend), Group: account.GroupName,
		Status: status, Enabled: &enabled, CreatedAt: parseClaudeGatewayTime(account.CreatedAt), LastActivityAt: parseClaudeGatewayTime(account.LastUsedAt),
		Requests: &requests, Tokens: &tokens, Cost: &cost, CostUnit: "usd", RPM: &rpm, ActiveSessions: &sessions,
		RateLimited: claudeGatewayRateLimited(account), ErrorMessage: firstNonEmpty(account.LastError, account.FailureKind, account.Stats.CooldownReason),
	}
}

func claudeGatewayStableID(value string) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(strings.TrimSpace(value)))
	id := int64(hash.Sum64() & ((1 << 63) - 1))
	if id == 0 {
		return 1
	}
	return id
}

func parseClaudeGatewayTime(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if numeric, err := strconv.ParseInt(value, 10, 64); err == nil {
		for numeric > 100_000_000_000 {
			numeric /= 1000
		}
		return numeric
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return parsed.Unix()
}

func claudeGatewayRateLimited(account claudeGatewayAccount) bool {
	if account.Stats.Cooldown && account.Stats.CooldownRemaining > 0 {
		return true
	}
	for _, value := range []string{account.FailureKind, account.LastError, account.Stats.CooldownReason} {
		value = strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(value, "rate_limit") || strings.Contains(value, "rate limit") || strings.Contains(value, "429") {
			return true
		}
	}
	return false
}

func (adapter claudeGatewayAdapter) Summary(ctx context.Context, connector *Connector, credential *CredentialMaterial, window TimeWindow) (*SummaryResult, error) {
	if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		return nil, ErrUnsupportedCapability
	}
	usage, err := fetchClaudeGatewayUsageSummary(ctx, connector, credential, window)
	if err != nil {
		return nil, err
	}
	return &SummaryResult{
		Window: window, Requests: supportedMetric(float64(usage.TotalRequests), "request"),
		Tokens: supportedMetric(float64(usage.TotalTokens), "token"), Cost: supportedMetric(float64(usage.TotalCost), "usd"),
		ErrorRate: unsupportedMetric("ratio"), Latency: unsupportedMetric("ms"),
	}, nil
}

func RefreshClaudeGatewayRealtime(ctx context.Context, instanceID int64) (ManagedRealtimeState, error) {
	lockValue, _ := newAPIRealtimeRefreshLocks.LoadOrStore(instanceID, &sync.Mutex{})
	refreshLock := lockValue.(*sync.Mutex)
	if !refreshLock.TryLock() {
		if state, ok := currentNewAPIRealtime(instanceID); ok {
			return state, nil
		}
		return ManagedRealtimeState{}, ErrUnsupportedCapability
	}
	defer refreshLock.Unlock()

	instance, _, connector, credential, err := observationClient(instanceID)
	if err != nil {
		return storeClaudeGatewayRealtimeFailure(instanceID, err), err
	}
	if instance.Kind != model.ManagedInstanceKindClaudeGateway {
		return ManagedRealtimeState{}, ErrUnsupportedCapability
	}
	accounts, err := fetchClaudeGatewayAccounts(ctx, connector, credential)
	if err != nil {
		if ShouldRecoverDataConnection(err) && RecoverDataConnection(ctx, instanceID, 0) == nil {
			instance, _, connector, credential, err = observationClient(instanceID)
			if err == nil {
				accounts, err = fetchClaudeGatewayAccounts(ctx, connector, credential)
			}
		}
		if err != nil {
			return storeClaudeGatewayRealtimeFailure(instanceID, err), err
		}
	}
	page := claudeGatewayInventoryPage(accounts)
	rpm, concurrency, sessions, available, reporting := 0.0, 0.0, 0, 0, 0
	for index, account := range accounts {
		if account.Stats.RPM >= 0 {
			rpm += float64(account.Stats.RPM)
			reporting++
		}
		if account.Stats.Concurrent > 0 {
			concurrency += float64(account.Stats.Concurrent)
		}
		if account.Stats.ActiveSessions > 0 {
			sessions += account.Stats.ActiveSessions
		}
		if page.Items[index].Enabled != nil && *page.Items[index].Enabled {
			available++
		}
	}
	now := common.GetTimestamp()
	state := ManagedRealtimeState{
		InstanceID: instanceID, ObservedAt: now, LastAttemptAt: now, StreamStatus: "connected",
		RPM: supportedMetric(rpm, "request/min"), ConcurrencyUsed: supportedMetric(concurrency, "concurrency"),
		ConcurrencyMax: unsupportedMetric("concurrency"), ConcurrencyStatus: model.ManagedInstanceCollectionSucceeded,
		AccountsTotal: len(accounts), AccountsAvailable: available, AccountsReporting: reporting,
		AccountsCollectionStatus: model.ManagedInstanceCollectionSucceeded, ActiveSessions: sessions, Accounts: page.Items,
	}
	if summary, summaryErr := fetchClaudeGatewayTodaySummary(ctx, connector, credential); summaryErr == nil {
		state.TodayCost = supportedMetric(float64(summary.TotalCost), "usd")
	} else if previous, ok := currentNewAPIRealtime(instanceID); ok {
		state.TodayCost = previous.TodayCost
	} else {
		state.TodayCost = unsupportedMetric("usd")
	}
	if overview, overviewErr := fetchClaudeGatewayOverview(ctx, connector, credential); overviewErr == nil {
		state.SuccessRate = supportedMetric(float64(overview.KPIs.SuccessRate), "ratio")
		state.SuccessRateSampleCount = float64(overview.KPIs.Total)
	} else if previous, ok := currentNewAPIRealtime(instanceID); ok {
		state.SuccessRate = previous.SuccessRate
		state.SuccessRateSampleCount = previous.SuccessRateSampleCount
	} else {
		state.SuccessRate = unsupportedMetric("ratio")
	}
	storeNewAPIRealtime(state)
	return state, nil
}

func storeClaudeGatewayRealtimeFailure(instanceID int64, err error) ManagedRealtimeState {
	state, _ := currentNewAPIRealtime(instanceID)
	state = markManagedRealtimeFailure(instanceID, state, err)
	storeNewAPIRealtime(state)
	return state
}

func collectClaudeGatewayAccountOutput(result *AccountOutputResult, items []InventoryItem) {
	result.Currency = "USD"
	for index, item := range items {
		requests, tokens, amount := 0.0, 0.0, 0.0
		if item.Requests != nil {
			requests = *item.Requests
		}
		if item.Tokens != nil {
			tokens = *item.Tokens
		}
		if item.Cost != nil {
			amount = *item.Cost
		}
		result.Items[index] = AccountOutputItem{
			Account: item, TotalRequests: requests, TotalTokens: tokens, Amount: amount,
			Currency: "USD", CollectionStatus: model.ManagedInstanceCollectionSucceeded,
		}
		result.CollectedAccounts++
		result.TotalRequests += requests
		result.TotalTokens += tokens
		result.TotalAmount += amount
	}
}
