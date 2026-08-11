package managedinstance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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
		headers, err := newAPIAuthHeaders(ctx, connector, detectedKind, credential)
		if err != nil {
			return nil, err
		}
		if credentialAccessScope(credential) == model.ManagedInstanceAccessUser {
			profileResponse, err := connector.DoJSON(ctx, http.MethodGet, "/api/user/self", headers, nil)
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
		adminResponse, err := connector.DoJSON(ctx, http.MethodGet, "/api/status/test", headers, nil)
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
	headers := make(http.Header)
	if kind == model.ManagedInstanceKindHuichuan {
		headers.Set("HUICHUAN-User", strconv.Itoa(envelope.Data.ID))
	} else {
		headers.Set("New-Api-User", strconv.Itoa(envelope.Data.ID))
	}
	return headers, nil
}

type sub2APIAdapter struct{}

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
		profileResponse, err := connector.DoJSON(ctx, http.MethodGet, "/api/v1/user/profile", headers, nil)
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
	versionResponse, err := connector.DoJSON(ctx, http.MethodGet, "/api/v1/admin/system/version", headers, nil)
	if err != nil {
		return nil, err
	}
	if versionResponse.StatusCode != http.StatusOK {
		return nil, probeHTTPError(versionResponse.StatusCode)
	}
	var version struct {
		Code any `json:"code"`
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(versionResponse.Body, &version); err != nil || !sub2SuccessCode(version.Code) {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: versionResponse.StatusCode}
	}
	result.Version = version.Data.Version
	result.Capabilities = append(result.Capabilities, "version.read", "accounts.list", "accounts.test", "accounts.toggle")
	if probeConfigEndpoint(ctx, connector, model.ManagedInstanceKindSub2API, headers) {
		result.Capabilities = append(result.Capabilities, "config.read", "config.apply")
	}
	return result, nil
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
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+envelope.Data.AccessToken)
	return headers, nil
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
