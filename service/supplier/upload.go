package supplier

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
)

var resourceID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type UploadInput struct {
	OAuthFlow     string   `json:"oauth_flow,omitempty"`
	BindingID     int64    `json:"binding_id"`
	Name          string   `json:"name"`
	ProxyMode     string   `json:"outbound_proxy_mode"`
	ProxyID       string   `json:"outbound_proxy_id"`
	GroupIDs      []string `json:"group_ids"`
	PolicyID      string   `json:"policy_template_id"`
	TemplateID    string   `json:"cc_template_id"`
	MaxRPM        int      `json:"max_rpm"`
	MaxTPM        int      `json:"max_tpm"`
	MaxConcurrent int      `json:"max_concurrent"`
	MaxSessions   int      `json:"max_sessions"`
}
type frozenUpload struct {
	Parameters UploadInput `json:"parameters"`
	State      string      `json:"state"`
}

func (in UploadInput) valid() bool {
	if in.OAuthFlow != "" && in.OAuthFlow != "login" && in.OAuthFlow != "setup_token" {
		return false
	}
	if strings.TrimSpace(in.Name) == "" || utf8.RuneCountInString(in.Name) > 64 || len(in.GroupIDs) > 100 {
		return false
	}
	if in.ProxyMode != "direct" && in.ProxyMode != "manual" && in.ProxyMode != "auto" {
		return false
	}
	if in.ProxyMode == "manual" && !resourceID.MatchString(in.ProxyID) {
		return false
	}
	for _, id := range append(append([]string{}, in.GroupIDs...), in.PolicyID, in.TemplateID) {
		if id != "" && !resourceID.MatchString(id) {
			return false
		}
	}
	return in.MaxRPM >= 0 && in.MaxRPM <= 1000000 && in.MaxTPM >= 0 && in.MaxTPM <= 1000000000 && in.MaxConcurrent >= 0 && in.MaxConcurrent <= 100000 && in.MaxSessions >= 0 && in.MaxSessions <= 100000
}
func (in UploadInput) payload() map[string]any {
	body := map[string]any{"name": strings.TrimSpace(in.Name), "provider": "anthropic", "oauth_flow": "login", "inference_backend": "native", "overwrite_existing": false, "outbound_proxy_mode": in.ProxyMode, "group_ids": in.GroupIDs, "max_rpm": in.MaxRPM, "max_tpm": in.MaxTPM, "max_concurrent": in.MaxConcurrent, "max_sessions": in.MaxSessions}
	if in.OAuthFlow != "" {
		body["oauth_flow"] = in.OAuthFlow
	}
	if in.ProxyMode == "manual" {
		body["outbound_proxy_id"] = in.ProxyID
	}
	if in.PolicyID != "" {
		body["policy_template_id"] = in.PolicyID
	}
	if in.TemplateID != "" {
		body["cc_template_id"] = in.TemplateID
	}
	return body
}
func (s *Service) StartUpload(ctx context.Context, p *Principal, in UploadInput) (map[string]any, error) {
	if !in.valid() {
		return nil, fail(400, "supplier_invalid_upload_parameters")
	}
	b, err := s.authorize(p, in.BindingID, "upload")
	if err != nil {
		return nil, err
	}
	c, err := cipher()
	if err != nil {
		return nil, fail(503, "supplier_encryption_unavailable")
	}
	remote, err := s.remoteFor(b)
	if err != nil {
		return nil, err
	}
	response, err := remote.Write(ctx, "POST", "account-upload/auth-url", in.payload())
	if err != nil {
		return nil, err
	}
	state, ok := response["state"].(string)
	authURL, _ := response["url"].(string)
	if !ok || state == "" || len(state) > 1024 || authURL == "" {
		return nil, fail(502, "supplier_invalid_oauth_response")
	}
	raw, err := token()
	if err != nil {
		return nil, err
	}
	frozen, err := json.Marshal(frozenUpload{Parameters: in, State: state})
	if err != nil {
		return nil, err
	}
	flow := model.SupplierOAuthFlow{TokenHash: digest(raw), SupplierID: p.Supplier.ID, SessionID: p.Session.ID, BindingID: b.ID, BindingRevision: b.Revision, ExpiresAt: s.Now().Add(10 * time.Minute).Unix()}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ? AND binding_id = ?", p.Session.ID, b.ID).Delete(&model.SupplierOAuthFlow{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&flow).Error; err != nil {
			return err
		}
		flow.Ciphertext, flow.KeyVersion, _, err = c.Encrypt(flow.ID, "supplier-oauth:v1", managedinstance.CredentialPayload{Secret: string(frozen)})
		if err != nil {
			return err
		}
		return tx.Save(&flow).Error
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"flow_id": raw, "url": authURL, "expires_at": flow.ExpiresAt}, nil
}
func callbackCode(callback, state string) (string, error) {
	callback = strings.TrimSpace(callback)
	if callback == "" || len(callback) > 8192 || strings.ContainsAny(callback, "\r\n") {
		return "", fail(400, "supplier_invalid_oauth_callback")
	}
	code, received := "", ""
	if strings.HasPrefix(callback, "https://") || strings.HasPrefix(callback, "http://") {
		u, err := url.Parse(callback)
		if err != nil {
			return "", fail(400, "supplier_invalid_oauth_callback")
		}
		query, parseErr := url.ParseQuery(u.RawQuery)
		if parseErr != nil || len(query["code"]) != 1 || len(query["state"]) != 1 || u.User != nil {
			return "", fail(400, "supplier_invalid_oauth_callback")
		}
		code = query.Get("code")
		received = query.Get("state")
	} else {
		code, received, _ = strings.Cut(callback, "#")
	}
	if code == "" || len(code) > 4096 || received != state {
		return "", fail(400, "supplier_oauth_state_mismatch")
	}
	return code, nil
}

func (s *Service) UploadBindingID(p *Principal, raw string) int64 {
	if len(raw) != 43 {
		return 0
	}
	var flow model.SupplierOAuthFlow
	if s.DB.Select("binding_id").Where("token_hash = ? AND supplier_id = ? AND session_id = ?", digest(raw), p.Supplier.ID, p.Session.ID).First(&flow).Error != nil {
		return 0
	}
	return flow.BindingID
}

func (s *Service) Exchange(ctx context.Context, p *Principal, flowToken, callback string) (map[string]any, error) {
	if len(flowToken) != 43 {
		return nil, fail(400, "supplier_invalid_oauth_flow")
	}
	var flow model.SupplierOAuthFlow
	if s.DB.Where("token_hash = ? AND supplier_id = ? AND session_id = ? AND expires_at > ? AND consumed_at = 0", digest(flowToken), p.Supplier.ID, p.Session.ID, s.Now().Unix()).First(&flow).Error != nil {
		return nil, fail(409, "supplier_oauth_flow_expired_or_used")
	}
	b, err := s.authorize(p, flow.BindingID, "upload")
	if err != nil {
		return nil, err
	}
	if b.Revision != flow.BindingRevision {
		return nil, fail(409, "supplier_binding_changed")
	}
	c, err := cipher()
	if err != nil {
		return nil, fail(503, "supplier_encryption_unavailable")
	}
	payload, err := c.Decrypt(flow.ID, "supplier-oauth:v1", flow.KeyVersion, flow.Ciphertext)
	if err != nil {
		return nil, fail(500, "supplier_invalid_oauth_flow")
	}
	var frozen frozenUpload
	if json.Unmarshal([]byte(payload.Secret), &frozen) != nil {
		return nil, fail(500, "supplier_invalid_oauth_flow")
	}
	code, err := callbackCode(callback, frozen.State)
	if err != nil {
		return nil, err
	}
	remote, err := s.remoteFor(b)
	if err != nil {
		return nil, err
	}
	// Consume before dispatch: an uncertain network result must never replay an exchange.
	r := s.DB.Model(&model.SupplierOAuthFlow{}).Where("id = ? AND consumed_at = 0 AND expires_at > ?", flow.ID, s.Now().Unix()).Updates(map[string]any{"consumed_at": s.Now().Unix(), "ciphertext": ""})
	if r.Error != nil {
		return nil, r.Error
	}
	if r.RowsAffected != 1 {
		return nil, fail(409, "supplier_oauth_flow_expired_or_used")
	}
	body := frozen.Parameters.payload()
	body["code"] = code
	body["pending_state"] = frozen.State
	defer s.invalidate(b)
	return remote.Write(ctx, "POST", "account-upload/exchange", body)
}
