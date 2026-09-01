package managedinstance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"net"
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

const (
	claudeGatewaySessionTTL           = 10 * time.Minute
	claudeGatewayAccountsMaxBodyBytes = int64(64 * 1024 * 1024)
	claudeGatewayBulkRequestTimeout   = 60 * time.Second
)

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
	OwnerUserID      string              `json:"owner_user_id"`
	CreatedAt        string              `json:"created_at"`
	LastUsedAt       string              `json:"last_used_at"`
	DisabledAt       string              `json:"disabled_at"`
	ExpiresAt        string              `json:"expires_at"`
	TotalRequests    claudeGatewayNumber `json:"total_requests"`
	TotalTokens      claudeGatewayNumber `json:"total_tokens"`
	TotalCost        claudeGatewayNumber `json:"total_cost"`
	Requests24H      claudeGatewayNumber `json:"req_24h"`
	Successful24H    claudeGatewayNumber `json:"ok_24h"`
	Limited24H       claudeGatewayNumber `json:"limited_24h"`
	RecoveryState    string              `json:"recovery_state"`
	UsageWindows     struct {
		Requests5H  *claudeGatewayNumber `json:"req_5h"`
		Tokens5H    *claudeGatewayNumber `json:"tokens_5h"`
		Cost5H      *claudeGatewayNumber `json:"cost_5h"`
		Requests7D  *claudeGatewayNumber `json:"req_7d"`
		Tokens7D    *claudeGatewayNumber `json:"tokens_7d"`
		Cost7D      *claudeGatewayNumber `json:"cost_7d"`
		Requests30D *claudeGatewayNumber `json:"req_30d"`
		Tokens30D   *claudeGatewayNumber `json:"tokens_30d"`
		Cost30D     *claudeGatewayNumber `json:"cost_30d"`
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

func (account *claudeGatewayAccount) UnmarshalJSON(data []byte) error {
	type plainAccount claudeGatewayAccount
	decoded := struct {
		plainAccount
		OwnerUserID json.RawMessage `json:"owner_user_id"`
	}{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*account = claudeGatewayAccount(decoded.plainAccount)
	ownerUserID, err := claudeGatewayFlexibleID(decoded.OwnerUserID)
	if err != nil {
		return err
	}
	account.OwnerUserID = ownerUserID
	return nil
}

type claudeGatewayVendor struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Status   string `json:"status"`
}

func (vendor *claudeGatewayVendor) UnmarshalJSON(data []byte) error {
	type plainVendor claudeGatewayVendor
	decoded := struct {
		plainVendor
		ID json.RawMessage `json:"id"`
	}{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*vendor = claudeGatewayVendor(decoded.plainVendor)
	id, err := claudeGatewayFlexibleID(decoded.ID)
	if err != nil {
		return err
	}
	vendor.ID = id
	return nil
}

func claudeGatewayFlexibleID(data json.RawMessage) (string, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return "", nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return "", err
		}
		return strings.TrimSpace(value), nil
	}
	var value json.Number
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	return strings.TrimSpace(value.String()), nil
}

type claudeGatewayTodaySummary struct {
	TotalCost    *claudeGatewayNumber `json:"total_cost"`
	TotalCost7D  *claudeGatewayNumber `json:"total_cost_7d"`
	TotalCost30D *claudeGatewayNumber `json:"total_cost_30d"`
	RequestCount claudeGatewayNumber  `json:"request_count"`
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
	return claudeGatewayDoJSONWithMaxBody(ctx, connector, credential, method, path, body, 0)
}

func claudeGatewayDoJSONWithMaxBody(ctx context.Context, connector *Connector, credential *CredentialMaterial, method, path string, body any, maxBodyBytes int64) (*ConnectorResponse, error) {
	headers, err := claudeGatewayAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.doJSONWithMaxBody(ctx, method, path, headers, body, maxBodyBytes)
	if err != nil || credential.AuthType != "account_password" || !authenticationRejected(response) {
		return response, err
	}
	invalidateClaudeGatewaySession(connector, credential, strings.TrimPrefix(headers.Get("Authorization"), "Bearer "))
	headers, err = claudeGatewayAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	return connector.doJSONWithMaxBody(ctx, method, path, headers, body, maxBodyBytes)
}

func (claudeGatewayAdapter) Inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind, cursor string) (*InventoryPage, error) {
	if strings.TrimSpace(cursor) != "" {
		return nil, ErrInvalidInstance
	}
	resourceKind = normalizeResourceKind(resourceKind, "account")
	if resourceKind != "account" || credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		return nil, ErrUnsupportedCapability
	}
	accounts, err := fetchClaudeGatewayAccountsBulk(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	vendors, vendorErr := fetchClaudeGatewayVendors(ctx, connector, credential)
	vendorObservedAt := common.GetTimestamp()
	if vendorErr != nil {
		vendors, vendorObservedAt = previousClaudeGatewayVendors(connector.instanceID)
	}
	page := claudeGatewayInventoryPage(accounts, vendors)
	page.VendorObservedAt = vendorObservedAt
	page.VendorCollectionStatus = model.ManagedInstanceCollectionSucceeded
	if vendorErr == nil && claudeGatewayUnmatchedVendorCount(page.Items) > 0 {
		page.VendorErrorCode = "claude_gateway_vendor_mapping_incomplete"
	}
	if vendorErr != nil {
		page.VendorCollectionStatus = model.ManagedInstanceCollectionFailed
		page.VendorErrorCode = managedInstanceObservationErrorCode(vendorErr)
		page.VendorStale = len(vendors) > 0
		if !page.VendorStale {
			page.VendorObservedAt = 0
		}
	}
	return page, nil
}

func claudeGatewayUnmatchedVendorCount(items []InventoryItem) int {
	count := 0
	for _, item := range items {
		if strings.TrimSpace(item.VendorID) != "" && item.VendorName == "未知供应商" {
			count++
		}
	}
	return count
}

func fetchClaudeGatewayAccounts(ctx context.Context, connector *Connector, credential *CredentialMaterial) ([]claudeGatewayAccount, error) {
	return fetchClaudeGatewayAccountsWithMode(ctx, connector, credential, false)
}

func fetchClaudeGatewayAccountsBulk(ctx context.Context, connector *Connector, credential *CredentialMaterial) ([]claudeGatewayAccount, error) {
	return fetchClaudeGatewayAccountsWithMode(ctx, connector, credential, true)
}

func fetchClaudeGatewayAccountsWithMode(ctx context.Context, connector *Connector, credential *CredentialMaterial, bulk bool) ([]claudeGatewayAccount, error) {
	request := claudeGatewayDoJSONWithMaxBody
	if bulk {
		request = claudeGatewayDoBulkJSON
	}
	response, err := request(
		ctx,
		connector,
		credential,
		http.MethodGet,
		"/api/admin/oauth-accounts",
		nil,
		claudeGatewayAccountsMaxBodyBytes,
	)
	if err != nil {
		return nil, claudeGatewayCollectionError("accounts", err)
	}
	if err := requireHTTPStatus(response); err != nil {
		return nil, claudeGatewayCollectionError("accounts", err)
	}
	var envelope struct {
		Accounts []claudeGatewayAccount `json:"accounts"`
	}
	if json.Unmarshal(response.Body, &envelope) != nil || envelope.Accounts == nil || len(envelope.Accounts) > managedInstanceInventoryMaxItems {
		return nil, &ProbeError{Code: "claude_gateway_accounts_invalid_response", StatusCode: response.StatusCode}
	}
	return envelope.Accounts, nil
}

func fetchClaudeGatewayVendors(ctx context.Context, connector *Connector, credential *CredentialMaterial) (map[string]claudeGatewayVendor, error) {
	response, err := claudeGatewayDoBulkJSON(
		ctx,
		connector,
		credential,
		http.MethodGet,
		"/api/admin/vendors?include_usage=true",
		nil,
		claudeGatewayAccountsMaxBodyBytes,
	)
	if err != nil {
		return nil, claudeGatewayCollectionError("vendors", err)
	}
	if err := requireHTTPStatus(response); err != nil {
		return nil, claudeGatewayCollectionError("vendors", err)
	}
	items, err := decodeClaudeGatewayVendors(response.Body)
	if err != nil || len(items) > managedInstanceInventoryMaxItems {
		return nil, &ProbeError{Code: "claude_gateway_vendors_invalid_response", StatusCode: response.StatusCode}
	}
	return claudeGatewayVendorMap(items), nil
}

func claudeGatewayDoBulkJSON(ctx context.Context, connector *Connector, credential *CredentialMaterial, method string, path string, body any, maxBodyBytes int64) (*ConnectorResponse, error) {
	headers, err := claudeGatewayAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.doJSONWithMaxBodyAndMinTimeout(ctx, method, path, headers, body, maxBodyBytes, claudeGatewayBulkRequestTimeout)
	if err != nil || credential.AuthType != "account_password" || !authenticationRejected(response) {
		return response, err
	}
	invalidateClaudeGatewaySession(connector, credential, strings.TrimPrefix(headers.Get("Authorization"), "Bearer "))
	headers, err = claudeGatewayAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	return connector.doJSONWithMaxBodyAndMinTimeout(ctx, method, path, headers, body, maxBodyBytes, claudeGatewayBulkRequestTimeout)
}

func decodeClaudeGatewayVendors(data []byte) ([]claudeGatewayVendor, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("empty Claude Gateway vendor response")
	}
	if data[0] == '[' {
		var items []claudeGatewayVendor
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, err
		}
		return items, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	for _, key := range []string{"items", "vendors"} {
		if raw, ok := envelope[key]; ok {
			return decodeClaudeGatewayVendorArray(raw)
		}
	}
	if raw, ok := envelope["data"]; ok {
		if items, err := decodeClaudeGatewayVendorArray(raw); err == nil {
			return items, nil
		}
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err != nil {
			return nil, err
		}
		for _, key := range []string{"items", "vendors"} {
			if items, ok := nested[key]; ok {
				return decodeClaudeGatewayVendorArray(items)
			}
		}
	}
	return nil, errors.New("Claude Gateway vendor list is missing")
}

func decodeClaudeGatewayVendorArray(data json.RawMessage) ([]claudeGatewayVendor, error) {
	var items []claudeGatewayVendor
	if err := json.Unmarshal(data, &items); err != nil || items == nil {
		if err == nil {
			err = errors.New("Claude Gateway vendor list is null")
		}
		return nil, err
	}
	return items, nil
}

func claudeGatewayCollectionError(stage string, err error) error {
	if err == nil {
		return nil
	}
	var networkError net.Error
	var probeError *ProbeError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded), errors.As(err, &networkError) && networkError.Timeout():
		return &ProbeError{Code: "claude_gateway_" + stage + "_timeout"}
	case errors.Is(err, ErrConnectorResponseLarge):
		return &ProbeError{Code: "claude_gateway_" + stage + "_response_too_large"}
	case errors.As(err, &probeError) && probeError.Code == ProbeErrorInvalidResponse:
		return &ProbeError{Code: "claude_gateway_" + stage + "_invalid_response", StatusCode: probeError.StatusCode}
	default:
		return err
	}
}

func claudeGatewayVendorMap(items []claudeGatewayVendor) map[string]claudeGatewayVendor {
	result := make(map[string]claudeGatewayVendor, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		item.ID = id
		item.Username = strings.TrimSpace(item.Username)
		item.Email = strings.TrimSpace(item.Email)
		result[id] = item
	}
	return result
}

func previousClaudeGatewayVendors(instanceID int64) (map[string]claudeGatewayVendor, int64) {
	if instanceID <= 0 || model.DB == nil || !model.DB.Migrator().HasTable(&model.ManagedAccountSnapshot{}) {
		return nil, 0
	}
	var snapshot model.ManagedAccountSnapshot
	query := model.DB.Where(
		"instance_id = ? AND snapshot_kind = ? AND range_key = ? AND observed_at > 0 AND payload <> ''",
		instanceID, model.ManagedAccountSnapshotKindInventory, "inventory",
	).Order("observed_at DESC").Limit(1).Find(&snapshot)
	if query.Error != nil || query.RowsAffected == 0 {
		return nil, 0
	}
	var page InventoryPage
	if json.Unmarshal([]byte(snapshot.Payload), &page) != nil {
		return nil, 0
	}
	result := make(map[string]claudeGatewayVendor)
	for _, item := range page.Items {
		id := strings.TrimSpace(item.VendorID)
		if id == "" || strings.TrimSpace(item.VendorName) == "" {
			continue
		}
		result[id] = claudeGatewayVendor{ID: id, Username: item.VendorName, Email: item.VendorEmail}
	}
	observedAt := page.VendorObservedAt
	if observedAt <= 0 && len(result) > 0 {
		observedAt = snapshot.ObservedAt
	}
	return result, observedAt
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

func claudeGatewayInventoryPage(accounts []claudeGatewayAccount, vendors map[string]claudeGatewayVendor) *InventoryPage {
	items := make([]InventoryItem, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, claudeGatewayAccountItem(account, vendors))
	}
	return &InventoryPage{ResourceKind: "account", Items: items, Total: len(items)}
}

func claudeGatewayAccountItem(account claudeGatewayAccount, vendors map[string]claudeGatewayVendor) InventoryItem {
	name := firstNonEmpty(account.Name, account.Email, account.ID)
	status := firstNonEmpty(account.HealthStatus, account.Status)
	rateLimited := claudeGatewayRateLimited(account)
	enabled := claudeGatewayAccountAvailable(account)
	requests, tokens, cost := float64(account.TotalRequests), float64(account.TotalTokens), float64(account.TotalCost)
	usageWindowDays := 0
	if account.UsageWindows.Requests30D != nil {
		requests = float64(*account.UsageWindows.Requests30D)
		usageWindowDays = 30
	}
	if account.UsageWindows.Tokens30D != nil {
		tokens = float64(*account.UsageWindows.Tokens30D)
		usageWindowDays = 30
	}
	if account.UsageWindows.Cost30D != nil {
		cost = float64(*account.UsageWindows.Cost30D)
		usageWindowDays = 30
	}
	requests24H := float64(account.Requests24H)
	successful24H := float64(account.Successful24H)
	limited24H := float64(account.Limited24H)
	rpm, sessions := account.Stats.RPM, account.Stats.ActiveSessions
	recoveryError := strings.TrimSpace(account.RecoveryState)
	if strings.EqualFold(recoveryError, "none") {
		recoveryError = ""
	}
	stableID := claudeGatewayStableID(account.ID)
	displayID := strings.TrimSpace(account.ID)
	if displayID == "" {
		displayID = strconv.FormatInt(stableID, 10)
	}
	item := InventoryItem{
		ID: stableID, IDText: displayID, Name: name, Email: strings.TrimSpace(account.Email), Note: strings.TrimSpace(account.Name), Ownership: strings.TrimSpace(account.GroupName),
		VendorID: strings.TrimSpace(account.OwnerUserID),
		Type:     firstNonEmpty(account.AccountType, account.AuthKind),
		Platform: firstNonEmpty(account.Provider, account.InferenceBackend), Group: account.GroupName,
		Status: status, Enabled: &enabled, CreatedAt: parseClaudeGatewayTime(account.CreatedAt), LastActivityAt: parseClaudeGatewayTime(account.LastUsedAt),
		DisabledAt: parseClaudeGatewayTime(account.DisabledAt), ExpiresAt: parseClaudeGatewayTime(account.ExpiresAt),
		Requests: &requests, Tokens: &tokens, Cost: &cost, CostUnit: "usd", UsageWindowDays: usageWindowDays,
		Requests24H: &requests24H, SuccessfulRequests24H: &successful24H, LimitedRequests24H: &limited24H,
		RPM: &rpm, ActiveSessions: &sessions, RateLimited: rateLimited,
		ErrorMessage: firstNonEmpty(account.LastError, account.FailureKind, account.Stats.CooldownReason, recoveryError),
	}
	applyClaudeGatewayVendor(&item, vendors)
	return item
}

func applyClaudeGatewayVendor(item *InventoryItem, vendors map[string]claudeGatewayVendor) {
	if item == nil {
		return
	}
	if item.VendorID == "" {
		item.VendorName = "平台自有"
		return
	}
	vendor, ok := vendors[item.VendorID]
	if !ok {
		item.VendorName = "未知供应商"
		return
	}
	item.VendorName = firstNonEmpty(vendor.Username, "未知供应商")
	item.VendorEmail = vendor.Email
}

func claudeGatewayAccountAvailable(account claudeGatewayAccount) bool {
	return strings.EqualFold(strings.TrimSpace(account.Status), "active") && !account.Stats.Cooldown
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
	for _, value := range []string{account.HealthStatus, account.FailureKind, account.RecoveryState, account.LastError, account.Stats.CooldownReason} {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "cooldown" || strings.Contains(value, "rate_limit") || strings.Contains(value, "rate limit") || strings.Contains(value, "429") {
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
	page := claudeGatewayInventoryPage(accounts, nil)
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
		AccountsCollectionStatus: model.ManagedInstanceCollectionSucceeded, AccountsObservedAt: now,
		ActiveSessions: sessions, ActiveSessionsObservedAt: now, ConcurrencyObservedAt: now, Accounts: page.Items,
	}
	previous, _ := currentNewAPIRealtime(instanceID)
	state.TodayCost, state.TodayCostObservedAt, state.TodayCostStale = cachedClaudeGatewayCost(
		previous.TodayCost, previous.TodayCostObservedAt, previous.TodayCostStale,
	)
	state.Cost7D, state.Cost7DObservedAt, state.Cost7DStale = cachedClaudeGatewayCost(
		previous.Cost7D, previous.Cost7DObservedAt, previous.Cost7DStale,
	)
	state.Cost30D, state.Cost30DObservedAt, state.Cost30DStale = cachedClaudeGatewayCost(
		previous.Cost30D, previous.Cost30DObservedAt, previous.Cost30DStale,
	)
	if overview, overviewErr := fetchClaudeGatewayOverview(ctx, connector, credential); overviewErr == nil {
		state.SuccessRate = supportedMetric(float64(overview.KPIs.SuccessRate), "ratio")
		state.SuccessRateSampleCount = float64(overview.KPIs.Total)
		state.SuccessRateObservedAt = now
	} else if previous, ok := currentNewAPIRealtime(instanceID); ok {
		state.SuccessRate = previous.SuccessRate
		state.SuccessRateSampleCount = previous.SuccessRateSampleCount
		state.SuccessRateObservedAt = previous.SuccessRateObservedAt
	} else {
		state.SuccessRate = unsupportedMetric("ratio")
	}
	storeNewAPIRealtime(state)
	return state, nil
}

func RefreshClaudeGatewayCosts(ctx context.Context, instanceID int64) (ManagedRealtimeState, error) {
	lockValue, _ := newAPIRealtimeRefreshLocks.LoadOrStore(instanceID, &sync.Mutex{})
	refreshLock := lockValue.(*sync.Mutex)
	refreshLock.Lock()
	defer refreshLock.Unlock()

	previous, _ := currentNewAPIRealtime(instanceID)
	instance, _, connector, credential, err := observationClient(instanceID)
	if err != nil {
		state := storeClaudeGatewayCostFailure(instanceID, previous)
		return state, err
	}
	if instance.Kind != model.ManagedInstanceKindClaudeGateway {
		return ManagedRealtimeState{}, ErrUnsupportedCapability
	}
	summary, err := fetchClaudeGatewayTodaySummary(ctx, connector, credential)
	if err != nil && ShouldRecoverDataConnection(err) && RecoverDataConnection(ctx, instanceID, 0) == nil {
		_, _, connector, credential, err = observationClient(instanceID)
		if err == nil {
			summary, err = fetchClaudeGatewayTodaySummary(ctx, connector, credential)
		}
	}
	if err != nil {
		state := storeClaudeGatewayCostFailure(instanceID, previous)
		return state, err
	}

	now := common.GetTimestamp()
	state := previous
	state.InstanceID = instanceID
	state.LastAttemptAt = now
	state.TodayCost, state.TodayCostObservedAt, state.TodayCostStale = claudeGatewayRealtimeCost(
		summary.TotalCost, previous.TodayCost, previous.TodayCostObservedAt, now,
	)
	state.Cost7D, state.Cost7DObservedAt, state.Cost7DStale = claudeGatewayRealtimeCost(
		summary.TotalCost7D, previous.Cost7D, previous.Cost7DObservedAt, now,
	)
	state.Cost30D, state.Cost30DObservedAt, state.Cost30DStale = claudeGatewayRealtimeCost(
		summary.TotalCost30D, previous.Cost30D, previous.Cost30DObservedAt, now,
	)
	storeNewAPIRealtime(state)
	sample := ManagedRealtimeHistorySample{}
	if !state.TodayCostStale {
		sample.TodayCost = metricHistoryValue(state.TodayCost)
	}
	if !state.Cost7DStale {
		sample.Cost7D = metricHistoryValue(state.Cost7D)
	}
	if !state.Cost30DStale {
		sample.Cost30D = metricHistoryValue(state.Cost30D)
	}
	if sample.TodayCost != nil || sample.Cost7D != nil || sample.Cost30D != nil {
		if recordErr := RecordManagedRealtimeHistorySample(ctx, instanceID, now, sample); recordErr != nil {
			ReportManagedRealtimeHistoryWriteError(ctx, instanceID, recordErr)
			return state, recordErr
		}
	}
	return state, nil
}

func cachedClaudeGatewayCost(value MetricSample, observedAt int64, stale bool) (MetricSample, int64, bool) {
	if value.CollectionStatus == model.ManagedInstanceCollectionSucceeded && value.Value != nil {
		return value, observedAt, stale
	}
	return unsupportedMetric("usd"), 0, true
}

func storeClaudeGatewayCostFailure(instanceID int64, previous ManagedRealtimeState) ManagedRealtimeState {
	previous.InstanceID = instanceID
	previous.LastAttemptAt = common.GetTimestamp()
	previous.TodayCost, previous.TodayCostObservedAt, previous.TodayCostStale = cachedClaudeGatewayCost(
		previous.TodayCost, previous.TodayCostObservedAt, true,
	)
	previous.Cost7D, previous.Cost7DObservedAt, previous.Cost7DStale = cachedClaudeGatewayCost(
		previous.Cost7D, previous.Cost7DObservedAt, true,
	)
	previous.Cost30D, previous.Cost30DObservedAt, previous.Cost30DStale = cachedClaudeGatewayCost(
		previous.Cost30D, previous.Cost30DObservedAt, true,
	)
	storeNewAPIRealtime(previous)
	return previous
}

func claudeGatewayRealtimeCost(value *claudeGatewayNumber, previous MetricSample, previousObservedAt, now int64) (MetricSample, int64, bool) {
	if value != nil && *value >= 0 {
		return supportedMetric(float64(*value), "usd"), now, false
	}
	if previous.CollectionStatus == model.ManagedInstanceCollectionSucceeded && previous.Value != nil {
		return previous, previousObservedAt, true
	}
	return unsupportedMetric("usd"), 0, true
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
