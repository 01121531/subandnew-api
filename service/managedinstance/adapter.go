package managedinstance

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
)

const (
	ProbeErrorAuthentication    = "authentication_failed"
	ProbeErrorPermission        = "permission_denied"
	ProbeErrorCompliance        = "compliance_required"
	ProbeErrorInvalidResponse   = "invalid_response"
	ProbeErrorRemoteHTTP        = "remote_http"
	ProbeErrorCredentialExpired = "credential_expired"
	ProbeErrorTwoFactorRequired = "two_factor_required"
)

type ProbeError struct {
	Code       string
	StatusCode int
}

func (err *ProbeError) Error() string {
	return "managed instance probe failed: " + err.Code
}

type CredentialMaterial struct {
	AuthType    string
	AccessScope string
	Secret      string
	UserID      string
}

type ProbeResult struct {
	Kind         string   `json:"kind"`
	Version      string   `json:"version"`
	SystemName   string   `json:"system_name"`
	StartTime    int64    `json:"start_time"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
	LatencyMS    int64    `json:"latency_ms"`
	CheckedAt    int64    `json:"checked_at"`
}

type InstanceAdapter interface {
	Kind() string
	Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error)
	Summary(ctx context.Context, connector *Connector, credential *CredentialMaterial, window TimeWindow) (*SummaryResult, error)
	Inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind string, cursor string) (*InventoryPage, error)
}

func adapterForKind(kind string) (InstanceAdapter, error) {
	switch kind {
	case model.ManagedInstanceKindNewAPI:
		return newAPIAdapter{configuredKind: kind}, nil
	case model.ManagedInstanceKindHuichuan:
		return newAPIAdapter{configuredKind: kind}, nil
	case model.ManagedInstanceKindSub2API:
		return sub2APIAdapter{}, nil
	case model.ManagedInstanceKindConductor:
		return conductorAdapter{}, nil
	case model.ManagedInstanceKindGeneric:
		return genericAdapter{}, nil
	default:
		return nil, fmt.Errorf("%w: unknown adapter kind", ErrInvalidInstance)
	}
}

type newAPIAdapter struct {
	configuredKind string
}

type newAPISession struct {
	mu        sync.Mutex
	userID    int
	cookies   []*http.Cookie
	expiresAt time.Time
}

const newAPISessionTTL = 30 * time.Minute

var newAPISessions sync.Map

type newAPIStatusResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Version    string `json:"version"`
		SystemName string `json:"system_name"`
		StartTime  int64  `json:"start_time"`
	} `json:"data"`
}

func (adapter newAPIAdapter) Kind() string { return adapter.configuredKind }

func (adapter newAPIAdapter) Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error) {
	response, err := connector.DoJSON(ctx, http.MethodGet, "/api/status", nil, nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, probeHTTPError(response.StatusCode)
	}
	var status newAPIStatusResponse
	if err := json.Unmarshal(response.Body, &status); err != nil || !status.Success || strings.TrimSpace(status.Data.Version) == "" {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	detectedKind := adapter.configuredKind
	if detectedKind == model.ManagedInstanceKindGeneric || detectedKind == "" {
		detectedKind = model.ManagedInstanceKindNewAPI
		if strings.Contains(strings.ToLower(status.Data.SystemName), "huichuan") {
			detectedKind = model.ManagedInstanceKindHuichuan
		}
	}
	capabilities := []string{"health.read", "version.read"}
	if credential != nil {
		if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
			profileResponse, err := newAPIDoJSON(ctx, connector, detectedKind, credential, http.MethodGet, "/api/user/self", nil)
			if err != nil {
				return nil, err
			}
			if profileResponse.StatusCode != http.StatusOK {
				return nil, probeHTTPError(profileResponse.StatusCode)
			}
			var profile struct {
				Success bool `json:"success"`
				Data    struct {
					ID int64 `json:"id"`
				} `json:"data"`
			}
			if json.Unmarshal(profileResponse.Body, &profile) != nil || !profile.Success || profile.Data.ID <= 0 {
				return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: profileResponse.StatusCode}
			}
			capabilities = append(capabilities, "profile.read", "usage.read")
			return &ProbeResult{
				Kind: detectedKind, Version: status.Data.Version, SystemName: status.Data.SystemName,
				StartTime: status.Data.StartTime, Status: model.ManagedInstanceStatusHealthy, Capabilities: capabilities,
			}, nil
		}
		adminResponse, err := newAPIDoJSON(ctx, connector, detectedKind, credential, http.MethodGet, "/api/status/test", nil)
		if err != nil {
			return nil, err
		}
		if adminResponse.StatusCode != http.StatusOK {
			return nil, probeHTTPError(adminResponse.StatusCode)
		}
		var adminStatus struct {
			Success bool `json:"success"`
		}
		if err := json.Unmarshal(adminResponse.Body, &adminStatus); err != nil {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: adminResponse.StatusCode}
		}
		if !adminStatus.Success {
			return nil, &ProbeError{Code: ProbeErrorAuthentication, StatusCode: adminResponse.StatusCode}
		}
		capabilities = append(capabilities, "channels.list", "channels.test", "channels.toggle")
		headers, err := newAPIAuthHeaders(ctx, connector, detectedKind, credential)
		if err != nil {
			return nil, err
		}
		if probeConfigEndpoint(ctx, connector, detectedKind, headers) {
			capabilities = append(capabilities, "config.read", "config.apply")
		}
	}
	return &ProbeResult{
		Kind: detectedKind, Version: status.Data.Version, SystemName: status.Data.SystemName,
		StartTime: status.Data.StartTime, Status: model.ManagedInstanceStatusHealthy, Capabilities: capabilities,
	}, nil
}

func newAPIAuthHeaders(ctx context.Context, connector *Connector, kind string, credential *CredentialMaterial) (http.Header, error) {
	if credential == nil || strings.TrimSpace(credential.Secret) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	if credential.AuthType == "account_password" {
		return loginNewAPI(ctx, connector, kind, credential)
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+credential.Secret)
	if strings.TrimSpace(credential.UserID) != "" {
		if kind == model.ManagedInstanceKindHuichuan {
			headers.Set("HUICHUAN-User", credential.UserID)
		} else {
			headers.Set("New-Api-User", credential.UserID)
		}
	} else if kind == model.ManagedInstanceKindHuichuan || credential.AuthType == "legacy_access_token" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	return headers, nil
}

func loginNewAPI(ctx context.Context, connector *Connector, kind string, credential *CredentialMaterial) (http.Header, error) {
	username := strings.TrimSpace(credential.UserID)
	if username == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	key := credentialSessionKey(connector, credential)
	stateValue, _ := newAPISessions.LoadOrStore(key, &newAPISession{})
	state := stateValue.(*newAPISession)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.userID > 0 && len(state.cookies) > 0 && time.Now().Before(state.expiresAt) {
		applyNewAPISession(connector, state.cookies)
		return newAPIUserHeaders(kind, state.userID), nil
	}
	response, err := connector.DoJSON(ctx, http.MethodPost, "/api/user/login", nil, map[string]string{
		"username": username,
		"password": credential.Secret,
	})
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, probeHTTPError(response.StatusCode)
	}
	var envelope struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			RequireTwoFactor bool `json:"require_2fa"`
			ID               int  `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	if envelope.Data.RequireTwoFactor {
		return nil, &ProbeError{Code: ProbeErrorTwoFactorRequired, StatusCode: response.StatusCode}
	}
	if !envelope.Success || envelope.Data.ID <= 0 {
		return nil, &ProbeError{Code: ProbeErrorAuthentication, StatusCode: response.StatusCode}
	}
	state.userID = envelope.Data.ID
	state.cookies = cloneHTTPCookies(connector.client.Jar.Cookies(connector.baseURL))
	state.expiresAt = time.Now().Add(newAPISessionTTL)
	return newAPIUserHeaders(kind, envelope.Data.ID), nil
}

func newAPIUserHeaders(kind string, userID int) http.Header {
	headers := make(http.Header)
	if kind == model.ManagedInstanceKindHuichuan {
		headers.Set("HUICHUAN-User", strconv.Itoa(userID))
	} else {
		headers.Set("New-Api-User", strconv.Itoa(userID))
	}
	return headers
}

func applyNewAPISession(connector *Connector, cookies []*http.Cookie) {
	if connector == nil || connector.client == nil || connector.client.Jar == nil || connector.baseURL == nil {
		return
	}
	connector.client.Jar.SetCookies(connector.baseURL, cloneHTTPCookies(cookies))
}

func cloneHTTPCookies(cookies []*http.Cookie) []*http.Cookie {
	cloned := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		copy := *cookie
		cloned = append(cloned, &copy)
	}
	return cloned
}

func credentialSessionKey(connector *Connector, credential *CredentialMaterial) string {
	baseURL := ""
	if connector != nil && connector.baseURL != nil {
		baseURL = connector.baseURL.String()
	}
	fingerprint := sha256.Sum256([]byte(credential.Secret))
	return baseURL + "\x00" + strings.ToLower(strings.TrimSpace(credential.UserID)) + "\x00" + fmt.Sprintf("%x", fingerprint)
}

func invalidateNewAPISession(connector *Connector, credential *CredentialMaterial) {
	_ = connector.resetCookies()
	stateValue, ok := newAPISessions.Load(credentialSessionKey(connector, credential))
	if !ok {
		return
	}
	state := stateValue.(*newAPISession)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.userID = 0
	state.cookies = nil
	state.expiresAt = time.Time{}
}

func invalidateAccountPasswordSession(connector *Connector, kind string, credential *CredentialMaterial) {
	if credential == nil || credential.AuthType != "account_password" {
		return
	}
	switch kind {
	case model.ManagedInstanceKindNewAPI, model.ManagedInstanceKindHuichuan:
		invalidateNewAPISession(connector, credential)
	case model.ManagedInstanceKindSub2API:
		invalidateSub2APISession(connector, credential, "")
	case model.ManagedInstanceKindConductor:
		invalidateConductorSession(connector, credential, "")
	case model.ManagedInstanceKindGeneric:
		invalidateNewAPISession(connector, credential)
		invalidateSub2APISession(connector, credential, "")
		invalidateConductorSession(connector, credential, "")
	}
}

func newAPIDoJSON(ctx context.Context, connector *Connector, kind string, credential *CredentialMaterial, method string, path string, requestBody any) (*ConnectorResponse, error) {
	headers, err := newAPIAuthHeaders(ctx, connector, kind, credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.DoJSON(ctx, method, path, headers, requestBody)
	if err != nil || credential.AuthType != "account_password" || !authenticationRejected(response) {
		return response, err
	}
	invalidateNewAPISession(connector, credential)
	headers, err = newAPIAuthHeaders(ctx, connector, kind, credential)
	if err != nil {
		return nil, err
	}
	return connector.DoJSON(ctx, method, path, headers, requestBody)
}

type sub2APIAdapter struct{}

type sub2APISession struct {
	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

const sub2APISessionTTL = 5 * time.Minute

var sub2APISessions sync.Map

func (sub2APIAdapter) Kind() string { return model.ManagedInstanceKindSub2API }

func (sub2APIAdapter) Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error) {
	healthResponse, err := connector.DoJSON(ctx, http.MethodGet, "/health", nil, nil)
	if err != nil {
		return nil, err
	}
	if healthResponse.StatusCode != http.StatusOK {
		return nil, probeHTTPError(healthResponse.StatusCode)
	}
	var health struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(healthResponse.Body, &health); err != nil || health.Status != "ok" {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: healthResponse.StatusCode}
	}
	result := &ProbeResult{
		Kind: model.ManagedInstanceKindSub2API, Status: model.ManagedInstanceStatusHealthy,
		Capabilities: []string{"health.read"},
	}
	if credential == nil {
		return result, nil
	}
	headers, err := sub2APIAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
		profileResponse, err := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/user/profile", nil)
		if err != nil {
			return nil, err
		}
		if profileResponse.StatusCode != http.StatusOK {
			return nil, probeHTTPError(profileResponse.StatusCode)
		}
		var profile struct {
			Code any `json:"code"`
			Data struct {
				ID int64 `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(profileResponse.Body, &profile) != nil || !sub2SuccessCode(profile.Code) || profile.Data.ID <= 0 {
			return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: profileResponse.StatusCode}
		}
		result.SystemName = "Sub2API"
		result.Capabilities = append(result.Capabilities, "profile.read", "usage.read", "quota.read")
		return result, nil
	}
	versionResponse, err := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/admin/system/version", nil)
	if err != nil {
		return nil, err
	}
	if versionResponse.StatusCode != http.StatusOK {
		if probeSub2AccountAccess(ctx, connector, credential) {
			result.SystemName = "Sub2API"
			result.Capabilities = append(result.Capabilities, "accounts.list", "usage.read")
			return result, nil
		}
		return nil, probeHTTPError(versionResponse.StatusCode)
	}
	var version struct {
		Code any `json:"code"`
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(versionResponse.Body, &version); err != nil || !sub2SuccessCode(version.Code) {
		if probeSub2AccountAccess(ctx, connector, credential) {
			result.SystemName = "Sub2API"
			result.Capabilities = append(result.Capabilities, "accounts.list", "usage.read")
			return result, nil
		}
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: versionResponse.StatusCode}
	}
	result.Version = version.Data.Version
	result.Capabilities = append(result.Capabilities, "version.read", "accounts.list", "accounts.test", "accounts.toggle")
	if probeConfigEndpoint(ctx, connector, model.ManagedInstanceKindSub2API, headers) {
		result.Capabilities = append(result.Capabilities, "config.read", "config.apply")
	}
	return result, nil
}

func probeSub2AccountAccess(ctx context.Context, connector *Connector, credential *CredentialMaterial) bool {
	query := url.Values{}
	query.Set("page", "1")
	query.Set("page_size", "1")
	response, err := sub2APIDoJSON(ctx, connector, credential, http.MethodGet, "/api/v1/admin/accounts?"+query.Encode(), nil)
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return false
	}
	data, err := sub2EnvelopeData(response)
	if err != nil {
		return false
	}
	_, _, _, err = extractInventoryRows(data)
	return err == nil
}

func credentialAccessScope(credential *CredentialMaterial) string {
	if credential != nil && credential.AccessScope == model.ManagedInstanceAccessUser {
		return model.ManagedInstanceAccessUser
	}
	return model.ManagedInstanceAccessAdmin
}

func probeConfigEndpoint(ctx context.Context, connector *Connector, kind string, headers http.Header) bool {
	path := "/api/option/"
	if kind == model.ManagedInstanceKindSub2API {
		path = "/api/v1/admin/settings"
	}
	response, err := connector.DoJSON(ctx, http.MethodGet, path, headers, nil)
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return false
	}
	if kind == model.ManagedInstanceKindSub2API {
		_, err = decodeSub2Settings(response.Body)
		return err == nil
	}
	var envelope struct {
		Success bool `json:"success"`
		Data    []struct {
			Key string `json:"key"`
		} `json:"data"`
	}
	return json.Unmarshal(response.Body, &envelope) == nil && envelope.Success && envelope.Data != nil
}

func sub2APIAuthHeaders(ctx context.Context, connector *Connector, credential *CredentialMaterial) (http.Header, error) {
	if credential == nil || strings.TrimSpace(credential.Secret) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	if credential.AuthType == "account_password" {
		return loginSub2API(ctx, connector, credential)
	}
	headers := make(http.Header)
	switch credential.AuthType {
	case "admin_token":
		headers.Set("x-api-key", credential.Secret)
	case "bearer_pat":
		headers.Set("Authorization", "Bearer "+credential.Secret)
	default:
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	return headers, nil
}

func loginSub2API(ctx context.Context, connector *Connector, credential *CredentialMaterial) (http.Header, error) {
	email := strings.TrimSpace(credential.UserID)
	if email == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	key := sub2APISessionKey(connector, credential)
	stateValue, _ := sub2APISessions.LoadOrStore(key, &sub2APISession{})
	state := stateValue.(*sub2APISession)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.accessToken != "" && time.Now().Before(state.expiresAt) {
		return sub2APIBearerHeaders(state.accessToken), nil
	}
	response, err := connector.DoJSON(ctx, http.MethodPost, "/api/v1/auth/login", nil, map[string]string{
		"email":    email,
		"password": credential.Secret,
	})
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, probeHTTPError(response.StatusCode)
	}
	var envelope struct {
		Code any `json:"code"`
		Data struct {
			AccessToken       string `json:"access_token"`
			RequiresTwoFactor bool   `json:"requires_2fa"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || !sub2SuccessCode(envelope.Code) {
		return nil, &ProbeError{Code: ProbeErrorAuthentication, StatusCode: response.StatusCode}
	}
	if envelope.Data.RequiresTwoFactor {
		return nil, &ProbeError{Code: ProbeErrorTwoFactorRequired, StatusCode: response.StatusCode}
	}
	if strings.TrimSpace(envelope.Data.AccessToken) == "" {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	state.accessToken = strings.TrimSpace(envelope.Data.AccessToken)
	state.expiresAt = time.Now().Add(sub2APISessionTTL)
	return sub2APIBearerHeaders(state.accessToken), nil
}

func sub2APIBearerHeaders(accessToken string) http.Header {
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+accessToken)
	return headers
}

func sub2APISessionKey(connector *Connector, credential *CredentialMaterial) string {
	return credentialSessionKey(connector, credential)
}

func invalidateSub2APISession(connector *Connector, credential *CredentialMaterial, accessToken string) {
	stateValue, ok := sub2APISessions.Load(sub2APISessionKey(connector, credential))
	if !ok {
		return
	}
	state := stateValue.(*sub2APISession)
	state.mu.Lock()
	defer state.mu.Unlock()
	if accessToken == "" || state.accessToken == accessToken {
		state.accessToken = ""
		state.expiresAt = time.Time{}
	}
}

func sub2APIDoJSON(ctx context.Context, connector *Connector, credential *CredentialMaterial, method string, path string, requestBody any) (*ConnectorResponse, error) {
	headers, err := sub2APIAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.DoJSON(ctx, method, path, headers, requestBody)
	if err != nil || credential.AuthType != "account_password" || !authenticationRejected(response) {
		return response, err
	}
	accessToken := strings.TrimPrefix(headers.Get("Authorization"), "Bearer ")
	invalidateSub2APISession(connector, credential, accessToken)
	headers, err = sub2APIAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	return connector.DoJSON(ctx, method, path, headers, requestBody)
}

func authenticationRejected(response *ConnectorResponse) bool {
	if response == nil {
		return false
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return true
	}
	var envelope struct {
		Code    any    `json:"code"`
		Success *bool  `json:"success"`
		Message string `json:"message"`
	}
	if json.Unmarshal(response.Body, &envelope) != nil {
		return false
	}
	code := strings.TrimSpace(fmt.Sprint(envelope.Code))
	if code == "401" {
		return true
	}
	message := strings.ToLower(strings.TrimSpace(envelope.Message))
	messageRejected := false
	for _, marker := range []string{
		"not logged in", "login required", "session expired", "token expired",
		"unauthorized", "authentication required", "未登录", "登录已过期", "会话已过期", "令牌已过期",
	} {
		if strings.Contains(message, marker) {
			messageRejected = true
			break
		}
	}
	if code == "403" {
		return messageRejected
	}
	return envelope.Success != nil && !*envelope.Success && messageRejected
}

type genericAdapter struct{}

func (genericAdapter) Kind() string { return model.ManagedInstanceKindGeneric }

func (genericAdapter) Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error) {
	result, newAPIErr := (newAPIAdapter{configuredKind: model.ManagedInstanceKindGeneric}).Probe(ctx, connector, credential)
	if newAPIErr == nil {
		return result, nil
	}
	if !canTryNextGenericAdapter(newAPIErr) {
		return nil, newAPIErr
	}
	result, sub2APIErr := (sub2APIAdapter{}).Probe(ctx, connector, credential)
	if sub2APIErr == nil {
		return result, nil
	}
	if !canTryNextGenericAdapter(sub2APIErr) {
		return nil, sub2APIErr
	}
	return (conductorAdapter{}).Probe(ctx, connector, credential)
}

func canTryNextGenericAdapter(err error) bool {
	var probeErr *ProbeError
	return errors.As(err, &probeErr) &&
		(probeErr.StatusCode == http.StatusNotFound || probeErr.Code == ProbeErrorInvalidResponse)
}

func probeHTTPError(statusCode int) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return &ProbeError{Code: ProbeErrorAuthentication, StatusCode: statusCode}
	case http.StatusForbidden:
		return &ProbeError{Code: ProbeErrorPermission, StatusCode: statusCode}
	case http.StatusLocked:
		return &ProbeError{Code: ProbeErrorCompliance, StatusCode: statusCode}
	default:
		return &ProbeError{Code: ProbeErrorRemoteHTTP, StatusCode: statusCode}
	}
}

func sub2SuccessCode(code any) bool {
	switch value := code.(type) {
	case float64:
		return value == 0
	case string:
		return value == "0"
	case nil:
		return false
	default:
		return false
	}
}
