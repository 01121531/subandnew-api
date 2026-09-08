package supplier

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
)

type Identity struct{ ID, Username string }

type Remote interface {
	Verify(context.Context) (Identity, error)
	Read(context.Context, string, url.Values) (map[string]any, error)
	Write(context.Context, string, string, any) (map[string]any, error)
}

// RemoteError never contains an upstream response, URL, or credential.
type RemoteError struct {
	Status int
	Code   string
}

func (e *RemoteError) Error() string { return "supplier upstream request failed" }

func remoteErr(status int, code string) error { return &RemoteError{Status: status, Code: code} }

const remoteMaxBody = 8 * 1024 * 1024

type remoteSession struct {
	gate      chan struct{}
	connector *managedinstance.Connector
	token     string
	identity  Identity
	expires   time.Time
}

type remoteClient struct {
	instance                       model.ManagedInstance
	identifier, password, expected string
	key                            string
}

// Bounded and wholly separate from the main collector's session namespace.
var remoteSessions = struct {
	sync.Mutex
	items map[string]*remoteSession
}{items: make(map[string]*remoteSession)}

func NewRemote(instance *model.ManagedInstance, identifier, password, namespace, expectedID string) (Remote, error) {
	if instance == nil || instance.Kind != model.ManagedInstanceKindClaudeGateway || strings.TrimSpace(identifier) == "" || password == "" || strings.TrimSpace(namespace) == "" || (expectedID != "" && !validRemoteID(expectedID)) {
		return nil, remoteErr(400, "invalid_configuration")
	}
	u, err := url.Parse(instance.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, remoteErr(400, "invalid_configuration")
	}
	// Structured encoding prevents namespace/credential delimiter collisions.
	keyBytes, _ := json.Marshal([]any{namespace, instance.Id, instance.BaseURL, instance.TLSVerify, strings.TrimSpace(identifier), password, expectedID})
	hash := sha256.Sum256(keyBytes)
	return &remoteClient{instance: *instance, identifier: strings.TrimSpace(identifier), password: password, expected: expectedID, key: hex.EncodeToString(hash[:])}, nil
}

func (r *remoteClient) session() (*remoteSession, error) {
	remoteSessions.Lock()
	defer remoteSessions.Unlock()
	if s := remoteSessions.items[r.key]; s != nil {
		return s, nil
	}
	c, err := managedinstance.NewSupplierConnector(&r.instance)
	if err != nil {
		return nil, remoteErr(502, "upstream_unavailable")
	}
	if len(remoteSessions.items) >= 256 {
		// Eviction does not mutate in-flight sessions.
		for key := range remoteSessions.items {
			delete(remoteSessions.items, key)
			break
		}
	}
	s := &remoteSession{connector: c, gate: make(chan struct{}, 1)}
	remoteSessions.items[r.key] = s
	return s, nil
}

func (r *remoteClient) Verify(ctx context.Context) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := r.session()
	if err != nil {
		return Identity{}, err
	}
	if err := s.acquire(ctx); err != nil {
		return Identity{}, err
	}
	defer s.release()
	if s.token == "" {
		err = r.login(ctx, s)
	} else {
		_, err = r.request(ctx, s, http.MethodGet, "/api/auth/me", nil)
	}
	if err != nil {
		return Identity{}, err
	}
	return s.identity, nil
}

func (s *remoteSession) acquire(ctx context.Context) error {
	select {
	case s.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return remoteErr(408, "upstream_timeout")
	}
}

func (s *remoteSession) release() { <-s.gate }

func rawRemote(ctx context.Context, s *remoteSession, method, path string, body any) (any, int, error) {
	headers := make(http.Header)
	if s.token != "" {
		headers.Set("Authorization", "Bearer "+s.token)
	}
	response, err := s.connector.DoJSON(ctx, method, path, headers, body)
	if err != nil {
		return nil, 0, remoteErr(502, "upstream_unavailable")
	}
	status := response.StatusCode
	if status < 200 || status >= 300 {
		code, localStatus := "upstream_rejected", 502
		if status == 401 || status == 403 {
			code, localStatus = "authentication_failed", status
		}
		if status == 429 {
			code, localStatus = "upstream_rate_limited", 429
		}
		if status == http.StatusNotFound && method == http.MethodGet && path == remotePoolSummaryPath {
			code, localStatus = "pool_summary_unsupported", http.StatusNotFound
		}
		return nil, status, remoteErr(localStatus, code)
	}
	if status == http.StatusNoContent {
		return map[string]any{}, status, nil
	}
	d := json.NewDecoder(bytes.NewReader(response.Body))
	d.UseNumber()
	var value any
	if d.Decode(&value) != nil {
		return nil, status, remoteErr(502, "invalid_response")
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return nil, status, remoteErr(502, "invalid_response")
	}
	for depth := 0; depth < 4; depth++ {
		m, ok := value.(map[string]any)
		if !ok {
			break
		}
		if success, ok := m["success"].(bool); ok && !success {
			return nil, status, remoteErr(502, "upstream_rejected")
		}
		_, healthResult := m["ok"].(bool)
		if m["error"] != nil && m["error"] != "" && !(healthResult && method == http.MethodPost && strings.HasPrefix(path, remoteProxiesPath+"/") && strings.HasSuffix(path, "/test")) {
			return nil, status, remoteErr(502, "upstream_rejected")
		}
		next, exists := m["data"]
		if !exists {
			break
		}
		// Preserve envelope pagination for lists wrapped in data.
		if a, ok := next.([]any); ok {
			next = map[string]any{"items": a}
		}
		if inner, ok := next.(map[string]any); ok {
			for _, key := range []string{"total", "total_rows", "page", "page_size", "limit", "pagination"} {
				if _, exists := inner[key]; !exists {
					if v, exists := m[key]; exists {
						inner[key] = v
					}
				}
			}
		}
		value = next
	}
	return value, status, nil
}

func (r *remoteClient) checkIdentity(s *remoteSession, value any) error {
	m, ok := value.(map[string]any)
	if !ok {
		return remoteErr(502, "invalid_response")
	}
	if user, ok := m["user"].(map[string]any); ok {
		m = user
	}
	id := remoteID(m["id"])
	if m["role"] != "vendor" || id == "" || (r.expected != "" && id != r.expected) || (s.identity.ID != "" && id != s.identity.ID) {
		return remoteErr(403, "supplier_identity_denied")
	}
	name, _ := m["username"].(string)
	if len(name) > 4096 {
		name = ""
	}
	for _, secret := range []string{r.password, s.token} {
		if secret != "" && strings.Contains(name, secret) {
			name = ""
		}
	}
	s.identity = Identity{ID: id, Username: name}
	return nil
}

func (r *remoteClient) acceptToken(ctx context.Context, s *remoteSession, value any) error {
	m, _ := value.(map[string]any)
	token, _ := m["accessToken"].(string)
	if token == "" {
		token, _ = m["access_token"].(string)
	}
	if token == "" || len(token) > 32768 || strings.ContainsAny(token, "\r\n") {
		return remoteErr(502, "invalid_response")
	}
	s.token = token
	me, _, err := rawRemote(ctx, s, http.MethodGet, "/api/auth/me", nil)
	if err == nil {
		err = r.checkIdentity(s, me)
	}
	if err != nil {
		s.token = ""
		_ = s.connector.ResetSupplierSession()
		return err
	}
	s.expires = time.Now().Add(10 * time.Minute)
	return nil
}

func (r *remoteClient) login(ctx context.Context, s *remoteSession) error {
	s.token = ""
	_ = s.connector.ResetSupplierSession()
	value, _, err := rawRemote(ctx, s, http.MethodPost, "/api/auth/admin-login", map[string]any{"identifier": r.identifier, "email": r.identifier, "password": r.password})
	if err != nil {
		return err
	}
	return r.acceptToken(ctx, s, value)
}

func (r *remoteClient) renew(ctx context.Context, s *remoteSession) (bool, error) {
	value, status, err := rawRemote(ctx, s, http.MethodPost, "/api/auth/refresh", nil)
	if err == nil {
		err = r.acceptToken(ctx, s, value)
		if err == nil {
			return false, nil
		}
		// Only an explicit authentication rejection permits re-login.
		if e, ok := err.(*RemoteError); !ok || e.Code == "supplier_identity_denied" || (e.Status != 401 && e.Status != 403) {
			return false, err
		}
	} else if status != 401 && status != 403 {
		return false, err
	}
	return true, r.login(ctx, s)
}

func (r *remoteClient) request(ctx context.Context, s *remoteSession, method, path string, body any) (any, error) {
	renewed := false
	relogged := false
	if s.token == "" {
		if err := r.login(ctx, s); err != nil {
			return nil, err
		}
	} else if !time.Now().Before(s.expires) {
		var err error
		if relogged, err = r.renew(ctx, s); err != nil {
			return nil, err
		}
		renewed = true
	}
	value, status, err := rawRemote(ctx, s, method, path, body)
	if (status == 401 || status == 403) && !renewed {
		if relogged, err = r.renew(ctx, s); err != nil {
			return nil, remoteRejectedRecovery(s, status, err)
		}
		value, status, err = rawRemote(ctx, s, method, path, body)
	}
	if (status == 401 || status == 403) && !relogged {
		if err := r.login(ctx, s); err != nil {
			return nil, remoteRejectedRecovery(s, status, err)
		}
		value, _, err = rawRemote(ctx, s, method, path, body)
	}
	if err == nil && path == "/api/auth/me" {
		err = r.checkIdentity(s, value)
	}
	if err != nil {
		if e, ok := err.(*RemoteError); ok && (e.Status == 401 || e.Status == 403) {
			s.token = ""
			_ = s.connector.ResetSupplierSession()
		}
		return nil, err
	}
	return value, nil
}

func remoteRejectedRecovery(s *remoteSession, status int, err error) error {
	s.token = ""
	_ = s.connector.ResetSupplierSession()
	// A known auth denial must not become a transient error eligible for stale
	// cache fallback just because its subsequent recovery request failed.
	if e, ok := err.(*RemoteError); ok && (e.Status == 401 || e.Status == 403) {
		return err
	}
	return remoteErr(status, "authentication_failed")
}
