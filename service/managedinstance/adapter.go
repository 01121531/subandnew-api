package managedinstance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/01121531/HUICHUAN-AI/model"
)

const (
	ProbeErrorAuthentication    = "authentication_failed"
	ProbeErrorPermission        = "permission_denied"
	ProbeErrorCompliance        = "compliance_required"
	ProbeErrorInvalidResponse   = "invalid_response"
	ProbeErrorRemoteHTTP        = "remote_http"
	ProbeErrorCredentialExpired = "credential_expired"
)

type ProbeError struct {
	Code       string
	StatusCode int
}

func (err *ProbeError) Error() string {
	return "managed instance probe failed: " + err.Code
}

type CredentialMaterial struct {
	AuthType string
	Secret   string
	UserID   string
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
}

func adapterForKind(kind string) (InstanceAdapter, error) {
	switch kind {
	case model.ManagedInstanceKindNewAPI:
		return newAPIAdapter{configuredKind: kind}, nil
	case model.ManagedInstanceKindHuichuan:
		return newAPIAdapter{configuredKind: kind}, nil
	case model.ManagedInstanceKindSub2API:
		return sub2APIAdapter{}, nil
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
		headers, err := newAPIAuthHeaders(detectedKind, credential)
		if err != nil {
			return nil, err
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
		capabilities = append(capabilities, "channels.list", "channels.test")
	}
	return &ProbeResult{
		Kind: detectedKind, Version: status.Data.Version, SystemName: status.Data.SystemName,
		StartTime: status.Data.StartTime, Status: model.ManagedInstanceStatusHealthy, Capabilities: capabilities,
	}, nil
}

func newAPIAuthHeaders(kind string, credential *CredentialMaterial) (http.Header, error) {
	if credential == nil || strings.TrimSpace(credential.Secret) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+credential.Secret)
	if kind == model.ManagedInstanceKindHuichuan || credential.AuthType == "legacy_access_token" {
		if strings.TrimSpace(credential.UserID) == "" {
			return nil, &ProbeError{Code: ProbeErrorAuthentication}
		}
		headers.Set("HUICHUAN-User", credential.UserID)
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
	headers, err := sub2APIAuthHeaders(credential)
	if err != nil {
		return nil, err
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
	result.Capabilities = append(result.Capabilities, "version.read", "accounts.list", "accounts.test")
	return result, nil
}

func sub2APIAuthHeaders(credential *CredentialMaterial) (http.Header, error) {
	if credential == nil || strings.TrimSpace(credential.Secret) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
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

type genericAdapter struct{}

func (genericAdapter) Kind() string { return model.ManagedInstanceKindGeneric }

func (genericAdapter) Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error) {
	result, newAPIErr := (newAPIAdapter{configuredKind: model.ManagedInstanceKindGeneric}).Probe(ctx, connector, credential)
	if newAPIErr == nil {
		return result, nil
	}
	var probeErr *ProbeError
	if !errors.As(newAPIErr, &probeErr) || (probeErr.StatusCode != http.StatusNotFound && probeErr.Code != ProbeErrorInvalidResponse) {
		return nil, newAPIErr
	}
	return (sub2APIAdapter{}).Probe(ctx, connector, credential)
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
