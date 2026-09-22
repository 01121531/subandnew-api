package managedinstance

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/01121531/subandnew-api/model"
)

type nevermoreAdapter struct{}

func (nevermoreAdapter) Kind() string { return model.ManagedInstanceKindNevermore }

func (nevermoreAdapter) Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error) {
	if credential == nil || credential.AuthType != "account_password" || strings.TrimSpace(credential.UserID) == "" || strings.TrimSpace(credential.Secret) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	response, err := connector.DoJSON(ctx, http.MethodPost, "/admin/v1/auth/login", nil, map[string]string{
		"username": credential.UserID,
		"password": credential.Secret,
	})
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, probeHTTPError(response.StatusCode)
	}
	var envelope struct {
		User struct {
			ID     int64    `json:"id"`
			Roles  []string `json:"roles"`
			Status string   `json:"status"`
		} `json:"user"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || envelope.User.ID <= 0 || len(envelope.User.Roles) == 0 {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	if strings.EqualFold(strings.TrimSpace(envelope.User.Status), "disabled") || strings.EqualFold(strings.TrimSpace(envelope.User.Status), "inactive") {
		return nil, &ProbeError{Code: ProbeErrorPermission, StatusCode: http.StatusForbidden}
	}
	return &ProbeResult{
		Kind:         model.ManagedInstanceKindNevermore,
		SystemName:   "Nevermore",
		Status:       model.ManagedInstanceStatusHealthy,
		AccessScope:  nevermoreAccessScope(envelope.User.Roles),
		Capabilities: []string{"health.read", "profile.read", "uploads.read"},
	}, nil
}

func (nevermoreAdapter) Summary(context.Context, *Connector, *CredentialMaterial, TimeWindow) (*SummaryResult, error) {
	return nil, ErrUnsupportedCapability
}

func (nevermoreAdapter) Inventory(context.Context, *Connector, *CredentialMaterial, string, string) (*InventoryPage, error) {
	return nil, ErrUnsupportedCapability
}

func nevermoreAccessScope(roles []string) string {
	for _, role := range roles {
		switch strings.ToUpper(strings.TrimSpace(role)) {
		case "ADMIN", "SUPER_ADMIN", "ROOT", "PLATFORM_ADMIN":
			return model.ManagedInstanceAccessAdmin
		}
	}
	return model.ManagedInstanceAccessUser
}

type routerAdapter struct{}

func (routerAdapter) Kind() string { return model.ManagedInstanceKindRouter }

func (routerAdapter) Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error) {
	if credential == nil || credential.AuthType != "account_password" || strings.TrimSpace(credential.UserID) == "" || strings.TrimSpace(credential.Secret) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	response, err := connector.DoJSON(ctx, http.MethodPost, "/api/user/login?turnstile=", nil, map[string]string{
		"username": credential.UserID,
		"password": credential.Secret,
	})
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, probeHTTPError(response.StatusCode)
	}
	var envelope struct {
		Success bool `json:"success"`
		Data    struct {
			ID     int64 `json:"id"`
			Role   int   `json:"role"`
			Status int   `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &envelope); err != nil || !envelope.Success || envelope.Data.ID <= 0 {
		return nil, &ProbeError{Code: ProbeErrorAuthentication, StatusCode: response.StatusCode}
	}
	accessScope, ok := routerAccessScope(envelope.Data.Role)
	if !ok || envelope.Data.Status == 0 {
		return nil, &ProbeError{Code: ProbeErrorPermission, StatusCode: http.StatusForbidden}
	}
	return &ProbeResult{
		Kind:         model.ManagedInstanceKindRouter,
		SystemName:   "Router",
		Status:       model.ManagedInstanceStatusHealthy,
		AccessScope:  accessScope,
		Capabilities: []string{"health.read", "profile.read", "usage.read", "channels.list"},
	}, nil
}

func (routerAdapter) Summary(context.Context, *Connector, *CredentialMaterial, TimeWindow) (*SummaryResult, error) {
	return nil, ErrUnsupportedCapability
}

func (routerAdapter) Inventory(context.Context, *Connector, *CredentialMaterial, string, string) (*InventoryPage, error) {
	return nil, ErrUnsupportedCapability
}

func routerAccessScope(role int) (string, bool) {
	switch role {
	case 5:
		return model.ManagedInstanceAccessChannelAdmin, true
	case 10, 100:
		return model.ManagedInstanceAccessAdmin, true
	default:
		return "", false
	}
}
