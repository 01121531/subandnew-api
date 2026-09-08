package supplier

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Write takes method before resource. Callers persist/freeze upload parameters
// alongside pending_state; this layer revalidates their current authorization.
func (r *remoteClient) Write(ctx context.Context, method, resource string, body any) (map[string]any, error) {
	path, id, action := "", "", ""
	switch {
	case resource == "proxies" && method == http.MethodPost:
		path, action = remoteProxiesPath+"/import", "import"
	case strings.HasPrefix(resource, "proxies/"):
		parts := strings.Split(resource, "/")
		if len(parts) < 2 || !validRemoteID(parts[1]) {
			return nil, remoteErr(400, "invalid_resource")
		}
		id = parts[1]
		if len(parts) == 3 && parts[2] == "test" && method == http.MethodPost {
			action = "test"
		}
		if len(parts) == 2 && method == http.MethodPatch {
			action = "status"
		}
		if len(parts) == 2 && method == http.MethodDelete {
			action = "delete"
		}
		if action == "" {
			return nil, remoteErr(400, "invalid_resource")
		}
		path = remoteProxiesPath + "/" + id
		if action == "test" {
			path += "/test"
		}
	case resource == "account-upload/auth-url" && method == http.MethodPost:
		path, action = remoteAccountsPath+"/auth-url", "auth-url"
	case resource == "account-upload/exchange" && method == http.MethodPost:
		path, action = remoteAccountsPath+"/exchange", "exchange"
	default:
		return nil, remoteErr(400, "invalid_resource")
	}
	m, err := remoteWriteBody(body)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	switch action {
	case "import":
		text, ok := m["text"].(string)
		if !ok || strings.TrimSpace(text) == "" || len(m) != 1 {
			return nil, remoteErr(400, "invalid_parameters")
		}
		payload = map[string]any{"text": text}
	case "status":
		if len(m) != 1 || (m["status"] != "enabled" && m["status"] != "disabled") {
			return nil, remoteErr(400, "invalid_parameters")
		}
		status := m["status"]
		if status == "enabled" {
			status = "active"
		}
		payload = map[string]any{"status": status}
	case "test", "delete":
		if len(m) != 0 {
			return nil, remoteErr(400, "invalid_parameters")
		}
		payload = map[string]any{}
	case "auth-url", "exchange":
		payload, err = remoteUploadBody(m, action == "exchange")
		if err != nil {
			return nil, err
		}
	}
	timeout := 30 * time.Second
	if action == "auth-url" || action == "exchange" || action == "import" {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	s, err := r.session()
	if err != nil {
		return nil, err
	}
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.release()
	if id != "" {
		proxies, err := r.all(ctx, s, remoteProxiesPath, "proxies", nil)
		if err != nil {
			return nil, err
		}
		var proxy map[string]any
		for _, item := range proxies {
			if remoteID(item["id"]) == id {
				proxy = item
				break
			}
		}
		if proxy == nil {
			return nil, remoteErr(403, "resource_not_authorized")
		}
		if action == "status" {
			owner, reason := remoteProxyOwnership(proxy, s.identity.ID)
			if owner != true {
				if reason == "proxy_ownership_conflict" {
					return nil, remoteErr(403, "resource_ownership_conflict")
				}
				if reason == "proxy_ownership_unknown" {
					return nil, remoteErr(403, "resource_ownership_unknown")
				}
				return nil, remoteErr(403, "resource_not_owned")
			}
		}
	}
	if action == "auth-url" || action == "exchange" {
		if err := r.authorizeUpload(ctx, s, payload); err != nil {
			return nil, err
		}
	}
	value, err := r.request(ctx, s, method, path, payload)
	if err != nil {
		return nil, err
	}
	response, err := remoteObject(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	switch action {
	case "auth-url":
		state, _ := response["state"].(string)
		rawURL, _ := response["url"].(string)
		if !validRemoteState(state) || !r.validOAuthURL(s, rawURL, state) {
			return nil, remoteErr(502, "invalid_oauth_response")
		}
		result = map[string]any{"url": rawURL, "state": state}
	case "exchange":
		result = map[string]any{"completed": true}
	case "import":
		result = remoteFields(response, "", "imported failed")
	case "test":
		result = remoteFields(response, "", "latency_ms")
		result["ok"] = nil
		if ok, exists := response["ok"].(bool); exists {
			result["ok"] = ok
		}
	default:
		result = map[string]any{"completed": true}
	}
	if action != "auth-url" {
		r.redact(s, result)
	}
	return result, nil
}

func remoteWriteBody(body any) (map[string]any, error) {
	if body == nil {
		return map[string]any{}, nil
	}
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > remoteMaxBody {
		return nil, remoteErr(400, "invalid_parameters")
	}
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.UseNumber()
	var m map[string]any
	if d.Decode(&m) != nil || m == nil {
		return nil, remoteErr(400, "invalid_parameters")
	}
	return m, nil
}

func remoteUploadBody(m map[string]any, exchange bool) (map[string]any, error) {
	result := map[string]any{"provider": "anthropic", "oauth_flow": "login", "inference_backend": "native", "overwrite": false, "overwrite_existing": false, "outbound_proxy_mode": "direct", "outbound_proxy_id": nil}
	for key, value := range m {
		switch key {
		case "provider", "oauth_flow", "inference_backend", "overwrite", "overwrite_existing":
			if value != result[key] {
				return nil, remoteErr(400, "invalid_parameters")
			}
		case "name":
			if value == nil {
				continue
			}
			s, ok := value.(string)
			if !ok || len([]rune(s)) > 64 || strings.ContainsAny(s, "\r\n\x00") {
				return nil, remoteErr(400, "invalid_parameters")
			}
			result[key] = s
		case "outbound_proxy_mode":
			if value != "direct" && value != "manual" && value != "auto" {
				return nil, remoteErr(400, "invalid_parameters")
			}
			result[key] = value
		case "outbound_proxy_id", "policy_template_id", "cc_template_id":
			if value == nil || value == "" {
				continue
			}
			id := remoteID(value)
			if id == "" {
				return nil, remoteErr(400, "invalid_parameters")
			}
			result[key] = id
		case "group_ids":
			if value == nil {
				result[key] = []string{}
				continue
			}
			values, ok := value.([]any)
			if !ok || len(values) > 100 {
				return nil, remoteErr(400, "invalid_parameters")
			}
			ids := make([]string, 0, len(values))
			seen := map[string]bool{}
			for _, v := range values {
				id := remoteID(v)
				if id == "" || seen[id] {
					return nil, remoteErr(400, "invalid_parameters")
				}
				seen[id] = true
				ids = append(ids, id)
			}
			result[key] = ids
		case "max_rpm", "max_tpm", "max_concurrent", "max_sessions":
			n, ok := remoteNumber(value).(float64)
			maximum := float64(1000000)
			if key == "max_tpm" {
				maximum = 1000000000
			}
			if key == "max_concurrent" || key == "max_sessions" {
				maximum = 100000
			}
			if !ok || n < 0 || n > maximum || math.Trunc(n) != n {
				return nil, remoteErr(400, "invalid_parameters")
			}
			result[key] = n
		case "code", "pending_state":
			if !exchange {
				return nil, remoteErr(400, "invalid_parameters")
			}
		default:
			return nil, remoteErr(400, "invalid_parameters")
		}
	}
	if result["outbound_proxy_mode"] == "manual" {
		if result["outbound_proxy_id"] == nil {
			return nil, remoteErr(400, "invalid_parameters")
		}
	} else if result["outbound_proxy_id"] != nil {
		return nil, remoteErr(400, "invalid_parameters")
	}
	if exchange {
		state, _ := m["pending_state"].(string)
		code, _ := m["code"].(string)
		if !validRemoteState(state) || len(code) == 0 || len(code) > 4096 || strings.ContainsAny(code, "\r\n\x00 /?:&") {
			return nil, remoteErr(400, "invalid_parameters")
		}
		parts := strings.Split(code, "#")
		if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] != state) {
			return nil, remoteErr(400, "invalid_parameters")
		}
		result["code"] = parts[0] + "#" + state
		result["pending_state"] = state
	}
	return result, nil
}

func (r *remoteClient) authorizeUpload(ctx context.Context, s *remoteSession, payload map[string]any) error {
	selected := map[string][]string{}
	if groups, ok := payload["group_ids"].([]string); ok {
		selected["groups"] = groups
	}
	for field, resource := range map[string]string{"outbound_proxy_id": "proxies", "policy_template_id": "policies", "cc_template_id": "templates"} {
		if id, ok := payload[field].(string); ok {
			selected[resource] = []string{id}
		}
	}
	for _, key := range []string{"groups", "policies", "templates", "proxies"} {
		ids := selected[key]
		if len(ids) == 0 {
			continue
		}
		items, err := r.all(ctx, s, remoteOptionPaths[key], key, remoteOptionQuery(key))
		if err != nil {
			return err
		}
		allowed := map[string]bool{}
		for _, item := range items {
			allowed[remoteID(item["id"])] = true
		}
		for _, id := range ids {
			if !allowed[id] {
				return remoteErr(403, "resource_not_authorized")
			}
		}
	}
	return nil
}

func validRemoteState(state string) bool {
	if len(state) == 0 || len(state) > 1024 {
		return false
	}
	for _, c := range state {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_-.~=", c)) {
			return false
		}
	}
	return true
}

func (r *remoteClient) validOAuthURL(s *remoteSession, raw, state string) bool {
	if len(raw) > 16384 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Port() != "" {
		return false
	}
	paths := map[string]string{"claude.ai": "/oauth/authorize", "console.anthropic.com": "/oauth/authorize", "platform.claude.com": "/oauth/authorize", "claude.com": "/cai/oauth/authorize"}
	if path, ok := paths[u.Host]; !ok || u.Path != path || u.RawPath != "" {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || q.Get("state") != state {
		return false
	}
	for key, values := range q {
		if len(values) != 1 {
			return false
		}
		switch key {
		case "client_id", "redirect_uri", "response_type", "scope", "state", "code_challenge", "code_challenge_method":
		case "code":
			if values[0] != "true" {
				return false
			}
		default:
			return false
		}
		for _, secret := range []string{r.password, s.token, r.identifier} {
			if secret != "" && strings.Contains(values[0], secret) {
				return false
			}
		}
	}
	if redirect := q.Get("redirect_uri"); redirect != "" {
		v, err := url.Parse(redirect)
		if err != nil || v.User != nil || v.RawQuery != "" || v.Fragment != "" || v.Scheme != "https" || v.Port() != "" || (v.Host != "console.anthropic.com" && v.Host != "platform.claude.com") || v.Path != "/oauth/code/callback" || v.RawPath != "" {
			return false
		}
	}
	return true
}
