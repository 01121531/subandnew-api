package managedinstance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/model"
)

type conductorAdapter struct{}

type conductorEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (conductorAdapter) Kind() string { return model.ManagedInstanceKindConductor }

func (conductorAdapter) Probe(ctx context.Context, connector *Connector, credential *CredentialMaterial) (*ProbeResult, error) {
	headers, err := conductorAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.DoJSON(ctx, http.MethodGet, "/api/v1/system/health", headers, nil)
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
			"health.read", "accounts.list", "users.list", "keys.list", "usage.read",
		},
	}, nil
}

func conductorAuthHeaders(ctx context.Context, connector *Connector, credential *CredentialMaterial) (http.Header, error) {
	if credential == nil || strings.TrimSpace(credential.Secret) == "" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	token := strings.TrimSpace(credential.Secret)
	if credential.AuthType == "account_password" {
		username := strings.TrimSpace(credential.UserID)
		if username == "" {
			return nil, &ProbeError{Code: ProbeErrorAuthentication}
		}
		response, err := connector.DoJSON(ctx, http.MethodPost, "/api/v1/auth/login", nil, map[string]string{
			"username": username,
			"password": credential.Secret,
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
		token = strings.TrimSpace(login.Token)
	} else if credential.AuthType != "bearer_pat" && credential.AuthType != "admin_token" {
		return nil, &ProbeError{Code: ProbeErrorAuthentication}
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	return headers, nil
}

func conductorEnvelopeData(response *ConnectorResponse) (json.RawMessage, error) {
	if err := requireHTTPStatus(response); err != nil {
		return nil, err
	}
	var envelope conductorEnvelope
	if json.Unmarshal(response.Body, &envelope) != nil || (envelope.Code != http.StatusOK && envelope.Code != http.StatusCreated) || len(envelope.Data) == 0 {
		return nil, &ProbeError{Code: ProbeErrorInvalidResponse, StatusCode: response.StatusCode}
	}
	return envelope.Data, nil
}

func (adapter conductorAdapter) Inventory(ctx context.Context, connector *Connector, credential *CredentialMaterial, resourceKind string, cursor string) (*InventoryPage, error) {
	resourceKind = normalizeResourceKind(resourceKind, "account")
	if resourceKind != "account" {
		return nil, ErrUnsupportedCapability
	}
	offset, err := conductorOffset(cursor)
	if err != nil {
		return nil, err
	}
	headers, err := conductorAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("offset", strconv.Itoa(offset))
	query.Set("limit", strconv.Itoa(managedInstanceInventoryPageSize))
	response, err := connector.DoJSON(ctx, http.MethodGet, "/api/v1/accounts?"+query.Encode(), headers, nil)
	if err != nil {
		return nil, err
	}
	data, err := conductorEnvelopeData(response)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Accounts []struct {
			AccountID string `json:"account_id"`
			Email     string `json:"email"`
			Label     string `json:"label"`
			AuthType  string `json:"auth_type"`
			Status    string `json:"status"`
			Health    string `json:"health"`
			Available bool   `json:"available"`
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
		status := strings.TrimSpace(account.Health)
		if status == "" {
			status = strings.TrimSpace(account.Status)
		}
		enabled := account.Available
		items = append(items, InventoryItem{ID: id, Name: name, Type: account.AuthType, Status: status, Enabled: &enabled})
	}
	nextCursor := ""
	if offset+len(payload.Accounts) < payload.Total {
		nextCursor = fmt.Sprintf("conductor:%d", offset+len(payload.Accounts))
	}
	return &InventoryPage{ResourceKind: "account", Items: items, Total: payload.Total, NextCursor: nextCursor}, nil
}

func conductorOffset(cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	const prefix = "conductor:"
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
	headers, err := conductorAuthHeaders(ctx, connector, credential)
	if err != nil {
		return nil, err
	}
	response, err := connector.DoJSON(ctx, http.MethodGet, "/api/v1/system/health", headers, nil)
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
	enabled := health.AccountsAvailable
	unhealthy := health.AccountsPaused + health.AccountsRejected
	unsupported := func(unit string) MetricSample {
		return MetricSample{Unit: unit, CollectionStatus: model.ManagedInstanceCollectionUnsupported}
	}
	result := &SummaryResult{
		Window:    window,
		Resources: []ResourceSummary{{ResourceKind: "account", Total: health.AccountsTotal, Enabled: &enabled, Unhealthy: &unhealthy}},
		Requests:  unsupported("request"), Tokens: unsupported("token"), Cost: unsupported("remote_currency"),
		ErrorRate: unsupported("ratio"), Latency: unsupported("ms"),
	}
	statsResponse, statsErr := connector.DoJSON(ctx, http.MethodGet, "/api/v1/system/stats", headers, nil)
	if statsErr != nil {
		return result, nil
	}
	statsData, statsErr := conductorEnvelopeData(statsResponse)
	if statsErr != nil {
		return result, nil
	}
	var stats struct {
		Usage struct {
			Recorded float64 `json:"recorded"`
		} `json:"usage"`
	}
	if json.Unmarshal(statsData, &stats) == nil {
		result.Requests = supportedMetric(stats.Usage.Recorded, "request")
	}
	return result, nil
}
