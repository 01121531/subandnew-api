package supplier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

const remoteTestPassword = "synthetic-password-for-contract-tests"
const remoteTestToken = "synthetic-access-token-for-contract-tests"

type remoteFixture struct {
	t                           *testing.T
	server                      *httptest.Server
	logins, profiles, refreshes atomic.Int64
	handle                      func(http.ResponseWriter, *http.Request) bool
}

func newRemoteFixture(t *testing.T, handle func(http.ResponseWriter, *http.Request) bool) *remoteFixture {
	t.Helper()
	t.Setenv("MANAGED_INSTANCE_ALLOWED_CIDRS", "127.0.0.0/8")
	t.Setenv("MANAGED_INSTANCE_ALLOWED_HOSTS", "127.0.0.1")
	t.Setenv("MANAGED_INSTANCE_ALLOWED_PORTS", "*")
	f := &remoteFixture{t: t, handle: handle}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/auth/admin-login":
			f.logins.Add(1)
		case "/api/auth/me":
			f.profiles.Add(1)
		case "/api/auth/refresh":
			f.refreshes.Add(1)
		}
		if f.handle != nil && f.handle(w, req) {
			return
		}
		switch req.URL.Path {
		case "/api/auth/admin-login":
			var m map[string]any
			if json.NewDecoder(req.Body).Decode(&m) != nil || m["identifier"] != "synthetic-vendor" || m["password"] != remoteTestPassword {
				t.Error("invalid synthetic login payload")
			}
			if req.Header.Get("Cookie") != "" {
				t.Error("login inherited another session's cookies")
			}
			http.SetCookie(w, &http.Cookie{Name: "refresh", Value: "synthetic-refresh-cookie", Path: "/api/auth", HttpOnly: true})
			fmt.Fprintf(w, `{"data":{"accessToken":%q}}`, remoteTestToken)
		case "/api/auth/me":
			if req.Header.Get("Authorization") != "Bearer "+remoteTestToken && req.Header.Get("Authorization") != "Bearer refreshed-token" {
				t.Error("profile missing supplier token")
			}
			io.WriteString(w, `{"data":{"user":{"id":9007199254740993,"username":"vendor display","role":"vendor","password":"DO NOT EXPOSE"}}}`)
		case "/api/auth/refresh":
			cookie, err := req.Cookie("refresh")
			if err != nil || cookie.Value != "synthetic-refresh-cookie" {
				t.Error("refresh cookie was not retained for /api/auth")
			}
			io.WriteString(w, `{"access_token":"refreshed-token"}`)
		default:
			t.Errorf("unexpected upstream route %s", req.URL.Path)
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *remoteFixture) client(namespace, expected string) *remoteClient {
	f.t.Helper()
	r, err := NewRemote(&model.ManagedInstance{Id: 17, Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: f.server.URL, TLSVerify: true}, "synthetic-vendor", remoteTestPassword, namespace, expected)
	require.NoError(f.t, err)
	return r.(*remoteClient)
}

func requireRemoteError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var e *RemoteError
	require.ErrorAs(t, err, &e)
	require.Equal(t, status, e.Status)
	if code != "" {
		require.Equal(t, code, e.Code)
	}
	require.Equal(t, "supplier upstream request failed", e.Error())
}

func TestRemoteLoginNamespaceAndFreshIdentity(t *testing.T) {
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path == remoteAccountsPath {
			io.WriteString(w, `{"accounts":[],"summary":{"total_rows":"0"}}`)
			return true
		}
		return false
	})
	r := f.client("binding-A", "9007199254740993")
	id, err := r.Verify(context.Background())
	require.NoError(t, err)
	require.Equal(t, Identity{ID: "9007199254740993", Username: "vendor display"}, id)
	_, err = r.Read(context.Background(), "accounts", nil)
	require.NoError(t, err)
	_, err = f.client("binding-A", "9007199254740993").Read(context.Background(), "accounts", nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, f.logins.Load())
	require.EqualValues(t, 1, f.profiles.Load())
	_, err = f.client("binding-B", "9007199254740993").Verify(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2, f.logins.Load())
}

func TestRemoteIdentityDenials(t *testing.T) {
	for _, tc := range []struct{ name, role, id, expected string }{
		{"admin", "admin", "v1", ""}, {"user", "user", "v1", ""}, {"missing role", "", "v1", ""}, {"wrong identity", "vendor", "v2", "v1"}, {"invalid id", "vendor", "../v1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path == "/api/auth/me" {
					json.NewEncoder(w).Encode(map[string]any{"role": tc.role, "id": tc.id})
					return true
				}
				return false
			})
			r := f.client(t.Name(), tc.expected)
			_, err := r.Read(context.Background(), "accounts", nil)
			requireRemoteError(t, err, 403, "supplier_identity_denied")
			s, err := r.session()
			require.NoError(t, err)
			require.Empty(t, s.token)
			require.EqualValues(t, 1, f.logins.Load())
			require.Zero(t, f.refreshes.Load())
		})
	}
}

func TestRemoteRefreshReloginAndRetryBudget(t *testing.T) {
	for _, mode := range []string{"refresh", "refresh-rejected", "refreshed-token-rejected", "always-rejected", "role-change", "id-change", "relogin-role-change"} {
		t.Run(mode, func(t *testing.T) {
			var reads atomic.Int64
			var f *remoteFixture
			f = newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path == remoteAccountsPath {
					n := reads.Add(1)
					if n == 1 || mode == "always-rejected" || (mode == "refreshed-token-rejected" && n == 2) {
						w.WriteHeader(401)
						io.WriteString(w, `{"error":"SECRET"}`)
					} else {
						io.WriteString(w, `{"items":[],"total":0}`)
					}
					return true
				}
				if req.URL.Path == "/api/auth/refresh" && (mode == "refresh-rejected" || mode == "relogin-role-change") {
					w.WriteHeader(403)
					return true
				}
				if req.URL.Path == "/api/auth/me" && ((mode == "role-change" || mode == "id-change") && f.refreshes.Load() > 0 || mode == "relogin-role-change" && f.logins.Load() > 1) {
					role, id := "admin", "9007199254740993"
					if mode == "id-change" {
						role, id = "vendor", "other-vendor"
					}
					json.NewEncoder(w).Encode(map[string]any{"role": role, "id": id})
					return true
				}
				return false
			})
			r := f.client(t.Name(), "")
			_, err := r.Read(context.Background(), "accounts", nil)
			switch mode {
			case "role-change", "id-change", "relogin-role-change":
				requireRemoteError(t, err, 403, "supplier_identity_denied")
				require.EqualValues(t, 1, reads.Load())
				s, e := r.session()
				require.NoError(t, e)
				require.Empty(t, s.token)
			case "always-rejected":
				requireRemoteError(t, err, 401, "authentication_failed")
				require.EqualValues(t, 3, reads.Load())
			default:
				require.NoError(t, err)
			}
			require.EqualValues(t, 1, f.refreshes.Load())
			require.LessOrEqual(t, f.logins.Load(), int64(2))
			if mode == "refresh" {
				require.EqualValues(t, 1, f.logins.Load())
				require.EqualValues(t, 2, f.profiles.Load())
			}
		})
	}
}

func TestRemoteExpiredSessionReverifies(t *testing.T) {
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path == remoteAccountsPath {
			io.WriteString(w, `{"accounts":[]}`)
			return true
		}
		return false
	})
	r := f.client(t.Name(), "")
	_, err := r.Verify(context.Background())
	require.NoError(t, err)
	s, err := r.session()
	require.NoError(t, err)
	s.expires = time.Now().Add(-time.Minute)
	_, err = r.Read(context.Background(), "accounts", nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, f.refreshes.Load())
	require.EqualValues(t, 2, f.profiles.Load())
}

func TestRemoteAccountResponseWrappersAndNulls(t *testing.T) {
	item := `{"id":9007199254740993,"name":"primary","total_cost":"1.25","total_tokens":"NaN","stats":{"daily_cost":"2.5"},"email":null,"access_token":"DO NOT EXPOSE","options":{"password":"DO NOT EXPOSE"}}`
	for _, body := range []string{`[` + item + `]`, `{"accounts":[` + item + `],"total":"1"}`, `{"items":[` + item + `],"pagination":{"total":"1"}}`, `{"data":{"accounts":[` + item + `],"summary":{"total_rows":"1"}}}`, `{"success":true,"total":"1","data":[` + item + `]}`} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path != remoteAccountsPath {
					return false
				}
				require.Equal(t, "1", req.URL.Query().Get("page_mode"))
				require.Equal(t, "20", req.URL.Query().Get("page_size"))
				require.Equal(t, "needle", req.URL.Query().Get("query"))
				io.WriteString(w, body)
				return true
			})
			result, err := f.client(t.Name(), "").Read(context.Background(), "accounts", url.Values{"page": {"2"}, "page_size": {"20"}, "search": {"needle"}})
			require.NoError(t, err)
			items := result["items"].([]map[string]any)
			require.Len(t, items, 1)
			require.Len(t, items[0], 10)
			require.Equal(t, "9007199254740993", items[0]["id"])
			require.Equal(t, 1.25, items[0]["total_cost"])
			require.Equal(t, 2.5, items[0]["today_cost"])
			require.Nil(t, items[0]["total_tokens"])
			require.Nil(t, items[0]["email"])
			require.Nil(t, items[0]["status"])
			require.Equal(t, 2, result["page"])
			require.Equal(t, 20, result["page_size"])
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "DO NOT EXPOSE")
			require.NotContains(t, string(encoded), "access_token")
		})
	}
}

func TestRemoteSummaryUsageProxyNormalization(t *testing.T) {
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		switch req.URL.Path {
		case remotePoolSummaryPath:
			w.WriteHeader(http.StatusNotFound)
		case remoteAccountsPath:
			io.WriteString(w, `{"data":{"summary":{"total_rows":"9","available_accounts":3,"rpm":"12","password":"hidden"}}}`)
		case "/api/admin/vendor-usage":
			io.WriteString(w, `{"data":{"days":[{"day":"2026-09-08","requests":"4","tokens":12,"cost":"0.5","error":"hidden"}],"accounts":[{"account_id":"a1","name":"one","requests":4,"tokens":"12","cost":null}]}}`)
		case remoteProxiesPath:
			io.WriteString(w, `{"proxies":[{"id":"p1","name":"proxy","scheme":"http","host":"proxy.example","port":"8080","status":"active","is_owner":true,"last_health_latency_ms":"3.5","password":"hidden","username":"hidden","display_url":"http://hidden:hidden@example.com"},{"id":"p2","host":"user:password@example.com","port":99999}],"total":"2"}`)
		default:
			return false
		}
		return true
	})
	r := f.client(t.Name(), "")
	summary, err := r.Read(context.Background(), "account-summary", nil)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"total_accounts": float64(9), "available_accounts": float64(3), "rpm": float64(12)}, summary)
	usage, err := r.Read(context.Background(), "usage", url.Values{"days": {"7"}})
	require.NoError(t, err)
	require.Equal(t, "2026-09-08", usage["days"].([]map[string]any)[0]["date"])
	require.Equal(t, "a1", usage["accounts"].([]map[string]any)[0]["id"])
	proxies, err := r.Read(context.Background(), "proxies", nil)
	require.NoError(t, err)
	items := proxies["items"].([]map[string]any)
	require.Len(t, items[0], 12)
	require.Equal(t, "enabled", items[0]["status"])
	require.Equal(t, 3.5, items[0]["latency_ms"])
	require.Nil(t, items[1]["host"])
	require.Nil(t, items[1]["port"])
	encoded, _ := json.Marshal([]any{summary, usage, proxies})
	require.NotContains(t, string(encoded), "hidden")
	require.NotContains(t, string(encoded), "password")
	require.EqualValues(t, 1, f.profiles.Load())
}

func TestRemoteOwnershipPaginatesAndOwnerOnlyStatus(t *testing.T) {
	for _, tc := range []struct {
		method, resource string
		body             any
		allowed          bool
	}{
		{"POST", "proxies/p2/test", nil, true}, {"PATCH", "proxies/p2", map[string]any{"status": "enabled"}, true}, {"DELETE", "proxies/p2", nil, true},
		{"POST", "proxies/p1/test", nil, true}, {"PATCH", "proxies/p1", map[string]any{"status": "disabled"}, false}, {"DELETE", "proxies/p1", nil, true}, {"DELETE", "proxies/foreign", nil, false},
	} {
		t.Run(tc.method+tc.resource, func(t *testing.T) {
			var pages, writes atomic.Int64
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path == remoteProxiesPath {
					pages.Add(1)
					if req.URL.Query().Get("page") == "1" {
						io.WriteString(w, `{"data":{"proxies":[{"id":"p1","is_owner":false}],"total":"2"}}`)
					} else {
						io.WriteString(w, `{"proxies":[{"id":"p2","is_owner":true}],"total":2}`)
					}
					return true
				}
				if strings.HasPrefix(req.URL.Path, remoteProxiesPath+"/") {
					writes.Add(1)
					if req.Method == "PATCH" {
						var p map[string]any
						json.NewDecoder(req.Body).Decode(&p)
						require.Equal(t, map[string]any{"status": "active"}, p)
					}
					io.WriteString(w, `{"ok":true,"latency_ms":"42","password":"hidden"}`)
					return true
				}
				return false
			})
			result, err := f.client(t.Name(), "").Write(context.Background(), tc.method, tc.resource, tc.body)
			if tc.allowed {
				require.NoError(t, err)
				require.EqualValues(t, 1, writes.Load())
				if strings.HasSuffix(tc.resource, "/test") {
					require.Equal(t, map[string]any{"ok": true, "latency_ms": float64(42)}, result)
				}
			} else {
				requireRemoteError(t, err, 403, "")
				require.Zero(t, writes.Load())
			}
			require.EqualValues(t, 2, pages.Load())
		})
	}
}

func TestRemoteOptionsAndUploadAuthorization(t *testing.T) {
	var writes atomic.Int64
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		for key, path := range remoteOptionPaths {
			if req.URL.Path == path {
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{key: []any{map[string]any{"id": key + "-id", "name": key, "password": "hidden", "policy": map[string]any{"secret": "hidden"}}}, "total": "1"}})
				return true
			}
		}
		if req.URL.Path == remoteAccountsPath+"/auth-url" || req.URL.Path == remoteAccountsPath+"/exchange" {
			writes.Add(1)
			var p map[string]any
			json.NewDecoder(req.Body).Decode(&p)
			require.Equal(t, "anthropic", p["provider"])
			require.Equal(t, "login", p["oauth_flow"])
			require.Equal(t, "native", p["inference_backend"])
			require.Equal(t, false, p["overwrite"])
			if strings.HasSuffix(req.URL.Path, "exchange") {
				require.Equal(t, "synthetic-code#synthetic-state", p["code"])
				io.WriteString(w, `{"data":{"id":"account1","access_token":"hidden"}}`)
			} else {
				io.WriteString(w, `{"url":"https://claude.ai/oauth/authorize?state=synthetic-state&redirect_uri=https%3A%2F%2Fconsole.anthropic.com%2Foauth%2Fcode%2Fcallback","state":"synthetic-state","code_verifier":"hidden"}`)
			}
			return true
		}
		return false
	})
	r := f.client(t.Name(), "")
	options, err := r.Read(context.Background(), "account-upload/options", nil)
	require.NoError(t, err)
	require.Len(t, options, 4)
	for key, list := range options {
		fields := 2
		if key == "proxies" {
			fields = 12
		} else if key == "policies" {
			fields = 3
		}
		require.Len(t, list.([]map[string]any)[0], fields)
	}
	p := map[string]any{"name": "new account", "group_ids": []string{"groups-id"}, "policy_template_id": "policies-id", "cc_template_id": "templates-id", "outbound_proxy_mode": "manual", "outbound_proxy_id": "proxies-id", "max_rpm": "100"}
	result, err := r.Write(context.Background(), "POST", "account-upload/auth-url", p)
	require.NoError(t, err)
	require.Len(t, result, 2)
	p["code"], p["pending_state"] = "synthetic-code", "synthetic-state"
	result, err = r.Write(context.Background(), "POST", "account-upload/exchange", p)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"completed": true}, result)
	p["group_ids"] = []string{"foreign-group"}
	_, err = r.Write(context.Background(), "POST", "account-upload/exchange", p)
	requireRemoteError(t, err, 403, "resource_not_authorized")
	require.EqualValues(t, 2, writes.Load())
}

func TestRemoteInvalidResourcesAndFrozenParametersNeverSend(t *testing.T) {
	f := newRemoteFixture(t, nil)
	r := f.client(t.Name(), "")
	for _, resource := range []string{"../accounts", "https://example.com", "accounts/1", "proxies/p1?admin=1", "proxies/%2e%2e/test", "proxies/a/b/test"} {
		_, err := r.Write(context.Background(), "POST", resource, nil)
		requireRemoteError(t, err, 400, "")
	}
	for _, p := range []map[string]any{
		{"provider": "openai"}, {"oauth_flow": "setup_token"}, {"inference_backend": "docker"}, {"overwrite": true}, {"owner_user_id": "other"}, {"options": map[string]any{"secret": true}},
		{"group_ids": []string{"../bad"}}, {"group_ids": []string{"g1", "g1"}}, {"outbound_proxy_mode": "manual"}, {"outbound_proxy_id": "p1"}, {"max_rpm": "NaN"}, {"cc_template_id": "a/b"},
	} {
		_, err := r.Write(context.Background(), "POST", "account-upload/auth-url", p)
		requireRemoteError(t, err, 400, "")
	}
	_, err := r.Write(context.Background(), "POST", "account-upload/exchange", map[string]any{"code": "code#wrong", "pending_state": "right"})
	requireRemoteError(t, err, 400, "")
	for _, q := range []url.Values{{"owner_user_id": {"foreign"}}, {"page": {"1", "2"}}, {"page_size": {"101"}}, {"group_id": {"foreign"}}} {
		_, err := r.Read(context.Background(), "accounts", q)
		requireRemoteError(t, err, 400, "")
	}
	require.Zero(t, f.logins.Load())
}

func TestRemoteOAuthURLSafety(t *testing.T) {
	for _, raw := range []string{
		"javascript:alert(1)", "http://claude.ai/oauth/authorize?state=s1", "https://claude.ai.evil.test/oauth/authorize?state=s1", "https://evil.test/oauth/authorize?state=s1",
		"https://user:pass@claude.ai/oauth/authorize?state=s1", "https://claude.ai:443/oauth/authorize?state=s1", "https://claude.ai/oauth/authorize?state=wrong",
		"https://claude.ai/oauth/authorize?state=s1&state=s1", "https://claude.ai/oauth/authorize?state=s1&access_token=secret", "https://claude.ai/oauth/authorize?state=s1&redirect_uri=https%3A%2F%2Fevil.test",
		"https://claude.ai/oauth/authorize?state=s1&client_id=" + remoteTestPassword, "https://claude.ai/oauth/authorize?state=s1&client_id=" + remoteTestToken,
		"https://claude.ai/oauth/authorize?state=s1&redirect_uri=javascript%3Aalert(1)",
	} {
		t.Run(raw, func(t *testing.T) {
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path == remoteAccountsPath+"/auth-url" {
					json.NewEncoder(w).Encode(map[string]any{"url": raw, "state": "s1"})
					return true
				}
				return false
			})
			result, err := f.client(t.Name(), "").Write(context.Background(), "POST", "account-upload/auth-url", nil)
			requireRemoteError(t, err, 502, "invalid_oauth_response")
			require.Nil(t, result)
		})
	}
}

func TestRemotePaginationFailuresDenyWrites(t *testing.T) {
	for _, mode := range []string{"duplicate", "truncated", "excessive", "invalid-id"} {
		t.Run(mode, func(t *testing.T) {
			var reads atomic.Int64
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path != remoteProxiesPath {
					return false
				}
				page := reads.Add(1)
				switch mode {
				case "duplicate":
					io.WriteString(w, `{"items":[{"id":"p1"}],"total":3}`)
				case "truncated":
					if page == 1 {
						io.WriteString(w, `{"items":[{"id":"p1"}],"total":2}`)
					} else {
						io.WriteString(w, `{"items":[],"total":2}`)
					}
				case "excessive":
					io.WriteString(w, `{"items":[{"id":"p1"}],"total":10001}`)
				case "invalid-id":
					io.WriteString(w, `{"items":[{"id":"../p1"}],"total":1}`)
				}
				return true
			})
			_, err := f.client(t.Name(), "").Write(context.Background(), "DELETE", "proxies/p1", nil)
			requireRemoteError(t, err, 502, "")
			require.LessOrEqual(t, reads.Load(), int64(2))
		})
	}
}

func TestRemoteUncertainWritesAreNotRetried(t *testing.T) {
	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			var writes atomic.Int64
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path == remoteProxiesPath {
					io.WriteString(w, `{"items":[{"id":"p1","is_owner":true}],"total":1}`)
					return true
				}
				if strings.HasPrefix(req.URL.Path, remoteProxiesPath+"/") {
					writes.Add(1)
					io.Copy(io.Discard, req.Body)
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						conn.Close()
					}
					return true
				}
				return false
			})
			resource := "proxies/p1"
			var body any
			if method == "POST" {
				resource = "proxies"
				body = map[string]any{"text": "http://synthetic:proxy-pass@proxy.example:8080"}
			}
			if method == "PATCH" {
				body = map[string]any{"status": "disabled"}
			}
			_, err := f.client(t.Name(), "").Write(context.Background(), method, resource, body)
			requireRemoteError(t, err, 502, "upstream_unavailable")
			require.EqualValues(t, 1, writes.Load())
			require.Zero(t, f.refreshes.Load())
		})
	}
}

func TestRemoteResponseLimitsErrorsAndRedirects(t *testing.T) {
	for _, mode := range []string{"large", "malformed", "trailing", "wrapped-error", "http-error", "redirect", "cross-origin"} {
		t.Run(mode, func(t *testing.T) {
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path != remoteAccountsPath {
					return false
				}
				switch mode {
				case "large":
					io.WriteString(w, `{"secret":"`+strings.Repeat("x", remoteMaxBody)+`"}`)
				case "malformed":
					io.WriteString(w, `SECRET invalid json`)
				case "trailing":
					io.WriteString(w, `{"items":[]} {"secret":"SECRET"}`)
				case "wrapped-error":
					io.WriteString(w, `{"data":{"success":false,"message":"SECRET"}}`)
				case "http-error":
					w.WriteHeader(500)
					io.WriteString(w, `{"password":"SECRET"}`)
				case "redirect":
					http.Redirect(w, req, "/api/admin/vendors", 307)
				case "cross-origin":
					http.Redirect(w, req, "https://example.invalid/steal", 307)
				}
				return true
			})
			result, err := f.client(t.Name(), "").Read(context.Background(), "accounts", nil)
			requireRemoteError(t, err, 502, "")
			require.Nil(t, result)
			require.NotContains(t, err.Error(), "SECRET")
		})
	}
}

func TestRemoteConcurrentSessionSingleLogin(t *testing.T) {
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path == remoteAccountsPath {
			io.WriteString(w, `{"items":[]}`)
			return true
		}
		return false
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.client("shared", "").Read(context.Background(), "accounts", nil)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, f.logins.Load())
	require.EqualValues(t, 1, f.profiles.Load())
}

func TestRemoteConfigurationAndSSRF(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "https://user:pass@example.com", "https://example.com?token=hidden", "https://example.com#secret", "//example.com"} {
		_, err := NewRemote(&model.ManagedInstance{Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: raw}, "a", "b", "c", "")
		requireRemoteError(t, err, 400, "invalid_configuration")
	}
	t.Setenv("MANAGED_INSTANCE_ALLOWED_CIDRS", "")
	t.Setenv("MANAGED_INSTANCE_ALLOWED_HOSTS", "")
	t.Setenv("MANAGED_INSTANCE_ALLOWED_PORTS", "*")
	r, err := NewRemote(&model.ManagedInstance{Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "http://127.0.0.1:1"}, "a", "b", t.Name(), "")
	require.NoError(t, err)
	_, err = r.Verify(context.Background())
	requireRemoteError(t, err, 502, "upstream_unavailable")
}

func TestRemoteFailedProxyTestAndImportResults(t *testing.T) {
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		switch req.URL.Path {
		case remoteProxiesPath:
			io.WriteString(w, `{"items":[{"id":"p1"}],"total":1}`)
		case remoteProxiesPath + "/p1/test":
			io.WriteString(w, `{"ok":false,"latency_ms":null,"error":"http://user:password@proxy.example:8080 failed"}`)
		case remoteProxiesPath + "/import":
			io.WriteString(w, `{"data":{"imported":"2","failed":"1","errors":["http://user:password@proxy.example:8080"],"proxies":[{"password":"hidden"}]}}`)
		default:
			return false
		}
		return true
	})
	r := f.client(t.Name(), "")
	result, err := r.Write(context.Background(), "POST", "proxies/p1/test", nil)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"ok": false, "latency_ms": nil}, result)
	result, err = r.Write(context.Background(), "POST", "proxies", map[string]any{"text": "http://user:password@proxy.example:8080"})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"imported": float64(2), "failed": float64(1)}, result)
}

func TestRemoteSessionWaitHonorsDeadline(t *testing.T) {
	f := newRemoteFixture(t, nil)
	r := f.client(t.Name(), "")
	s, err := r.session()
	require.NoError(t, err)
	require.NoError(t, s.acquire(context.Background()))
	defer s.release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = r.Read(ctx, "accounts", nil)
	requireRemoteError(t, err, 408, "upstream_timeout")
	require.Less(t, time.Since(started), time.Second)
	require.Zero(t, f.logins.Load())
}

func TestRemoteInitialAuthenticationFailureIsSafe(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path != "/api/auth/admin-login" {
					return false
				}
				w.WriteHeader(status)
				io.WriteString(w, `{"error":"invalid password: DO NOT EXPOSE"}`)
				return true
			})
			_, err := f.client(t.Name(), "").Verify(context.Background())
			requireRemoteError(t, err, status, "authentication_failed")
			require.Zero(t, f.profiles.Load())
		})
	}
}

func TestRemoteRefreshNetworkFailureDoesNotReplayWrite(t *testing.T) {
	var writes atomic.Int64
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path == remoteProxiesPath+"/import" {
			writes.Add(1)
			w.WriteHeader(401)
			return true
		}
		if req.URL.Path == "/api/auth/refresh" {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return true
		}
		return false
	})
	_, err := f.client(t.Name(), "").Write(context.Background(), "POST", "proxies", map[string]any{"text": "http://proxy.example:8080"})
	requireRemoteError(t, err, 401, "authentication_failed")
	require.EqualValues(t, 1, f.logins.Load())
	require.EqualValues(t, 1, f.refreshes.Load())
	require.EqualValues(t, 1, writes.Load())
}

func TestRemoteWriteBodySizeAndCoreFrozenPayload(t *testing.T) {
	_, err := remoteWriteBody(map[string]any{"text": strings.Repeat("x", remoteMaxBody)})
	requireRemoteError(t, err, 400, "invalid_parameters")
	encoded, err := remoteWriteBody(map[string]any{"group_ids": []string(nil), "overwrite_existing": false, "max_tpm": 1000000000})
	require.NoError(t, err)
	payload, err := remoteUploadBody(encoded, false)
	require.NoError(t, err)
	require.Equal(t, []string{}, payload["group_ids"])
	require.Equal(t, false, payload["overwrite_existing"])
	require.Equal(t, false, payload["overwrite"])
	require.Equal(t, float64(1000000000), payload["max_tpm"])
}

func TestRemoteVerificationRevokesChangedIdentity(t *testing.T) {
	var changed atomic.Bool
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path == "/api/auth/me" && changed.Load() {
			io.WriteString(w, `{"id":"another-vendor","role":"vendor"}`)
			return true
		}
		return false
	})
	r := f.client(t.Name(), "")
	_, err := r.Verify(context.Background())
	require.NoError(t, err)
	changed.Store(true)
	_, err = r.Verify(context.Background())
	requireRemoteError(t, err, 403, "supplier_identity_denied")
	s, err := r.session()
	require.NoError(t, err)
	require.Empty(t, s.token)
	_, err = r.Read(context.Background(), "accounts", nil)
	requireRemoteError(t, err, 403, "supplier_identity_denied")
}

func TestRemoteSummaryDoesNotSubstituteGlobalPoolMetrics(t *testing.T) {
	var poolCalls atomic.Int64
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		switch req.URL.Path {
		case remoteAccountsPath + "/pool-summary":
			poolCalls.Add(1)
			require.Equal(t, "Bearer "+remoteTestToken, req.Header.Get("Authorization"))
			require.Empty(t, req.URL.RawQuery)
			io.WriteString(w, `{"data":{"global":{"rpm":"9999","concurrent":"40","secret":"DO NOT EXPOSE"},"pool":{"total_accounts":8000,"available_accounts":"7000"}}}`)
		case remoteAccountsPath:
			require.Equal(t, "Bearer "+remoteTestToken, req.Header.Get("Authorization"))
			require.Equal(t, url.Values{"page": {"1"}, "page_mode": {"1"}, "page_size": {"1"}}, req.URL.Query())
			io.WriteString(w, `{"data":{"accounts":[],"summary":{"total_rows":"12","available_accounts":"3","rpm":"7"},"global":{"rpm":9999},"pool":{"available_accounts":7000}}}`)
		default:
			return false
		}
		return true
	})
	result, err := f.client(t.Name(), "9007199254740993").Read(context.Background(), "account-summary", nil)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"total_accounts": float64(12), "available_accounts": float64(3), "rpm": float64(7), "pool_rpm": float64(9999), "pool_concurrent": float64(40), "pool_available_accounts": float64(7000)}, result)
	require.EqualValues(t, 1, poolCalls.Load())
}

func TestRemotePoolSummaryOptionalFieldsAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body         string
		status, wantStatus int
	}{
		{"unsupported", `{"error":"DO NOT EXPOSE"}`, 404, 0},
		{"missing", `{"data":{"global":{},"pool":{}}}`, 200, 0},
		{"zero-and-null", `{"global":{"rpm":"0"},"pool":{"available_accounts":null}}`, 200, 0},
		{"malformed", `{"data":0}`, 200, 502},
		{"malformed-section", `{"global":false}`, 200, 502},
		{"server-error", `{"error":"DO NOT EXPOSE"}`, 500, 502},
		{"rate-limited", `{"error":"DO NOT EXPOSE"}`, 429, 429},
		{"unauthorized", `{"error":"DO NOT EXPOSE"}`, 401, 401},
		{"forbidden", `{"error":"DO NOT EXPOSE"}`, 403, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path == remoteAccountsPath {
					io.WriteString(w, `{"summary":{"total_rows":"12","available_accounts":"3","rpm":"7"}}`)
					return true
				}
				if req.URL.Path == remotePoolSummaryPath {
					w.WriteHeader(tc.status)
					io.WriteString(w, tc.body)
					return true
				}
				return false
			})
			result, err := f.client(t.Name(), "9007199254740993").Read(context.Background(), "account-summary", nil)
			if tc.wantStatus != 0 {
				requireRemoteError(t, err, tc.wantStatus, "")
				require.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.Equal(t, float64(12), result["total_accounts"])
			require.Equal(t, float64(3), result["available_accounts"])
			require.Equal(t, float64(7), result["rpm"])
			if tc.name == "zero-and-null" {
				require.Equal(t, float64(0), result["pool_rpm"])
				require.Contains(t, result, "pool_available_accounts")
				require.Nil(t, result["pool_available_accounts"])
				require.NotContains(t, result, "pool_concurrent")
			} else {
				require.Len(t, result, 3)
				for _, key := range []string{"pool_rpm", "pool_concurrent", "pool_available_accounts"} {
					require.NotContains(t, result, key)
				}
			}
		})
	}
}

func TestRemoteOwnSummaryFailureIsNotOptional(t *testing.T) {
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path == remoteAccountsPath {
			w.WriteHeader(404)
			return true
		}
		return false
	})
	result, err := f.client(t.Name(), "").Read(context.Background(), "account-summary", nil)
	requireRemoteError(t, err, 502, "upstream_rejected")
	require.Nil(t, result)
}

func TestRemoteProxyOptionsFiltersSanitizationAndUploadOwnership(t *testing.T) {
	var filteredPages, authWrites atomic.Int64
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path == remoteProxiesPath {
			q := req.URL.Query()
			require.Equal(t, "Bearer "+remoteTestToken, req.Header.Get("Authorization"))
			if q.Get("available_for_binding") == "1" {
				filteredPages.Add(1)
				require.Equal(t, "native", q.Get("inference_backend"))
				require.Equal(t, "100", q.Get("limit"))
				require.Len(t, q, 4)
				if q.Get("page") == "1" {
					io.WriteString(w, `{"data":{"proxies":[{"id":"p1","name":"Native proxy","scheme":"http","host":"proxy.example","port":"8080","status":"active","is_owner":false,"password":"DO NOT EXPOSE","username":"DO NOT EXPOSE","display_url":"http://user:secret@proxy.example:8080"}],"total":"2"}}`)
				} else {
					require.Equal(t, "2", q.Get("page"))
					io.WriteString(w, `{"proxies":[{"id":"p2","name":"Second page","host":"user:secret@proxy.example","port":"70000","last_health_latency_ms":"5","is_owner":true}],"total":2}`)
				}
			} else {
				require.Empty(t, q.Get("inference_backend"))
				io.WriteString(w, `{"proxies":[{"id":"docker-or-unavailable","name":"Not available for native binding","is_owner":true}],"total":1}`)
			}
			return true
		}
		for key, path := range remoteOptionPaths {
			if req.URL.Path == path {
				json.NewEncoder(w).Encode(map[string]any{key: []any{}})
				return true
			}
		}
		if req.URL.Path == remoteAccountsPath+"/auth-url" {
			authWrites.Add(1)
			io.WriteString(w, `{"url":"https://claude.ai/oauth/authorize?state=s1","state":"s1"}`)
			return true
		}
		return false
	})
	r := f.client(t.Name(), "9007199254740993")
	options, err := r.Read(context.Background(), "account-upload/options", nil)
	require.NoError(t, err)
	proxies := options["proxies"].([]map[string]any)
	require.Len(t, proxies, 2)
	require.Equal(t, "proxy.example", proxies[0]["host"])
	require.Equal(t, float64(8080), proxies[0]["port"])
	require.Equal(t, "enabled", proxies[0]["status"])
	require.Nil(t, proxies[1]["host"])
	require.Nil(t, proxies[1]["port"])
	require.Equal(t, float64(5), proxies[1]["latency_ms"])
	encoded, err := json.Marshal(options)
	require.NoError(t, err)
	for _, secret := range []string{"DO NOT EXPOSE", "secret", "password", "username", "display_url"} {
		require.NotContains(t, string(encoded), secret)
	}
	require.EqualValues(t, 2, filteredPages.Load())
	_, err = r.Write(context.Background(), "POST", "account-upload/auth-url", map[string]any{"outbound_proxy_mode": "manual", "outbound_proxy_id": "p2"})
	require.NoError(t, err)
	require.EqualValues(t, 4, filteredPages.Load(), "upload rechecks both filtered pages")
	_, err = r.Write(context.Background(), "POST", "account-upload/auth-url", map[string]any{"outbound_proxy_mode": "manual", "outbound_proxy_id": "docker-or-unavailable"})
	requireRemoteError(t, err, 403, "resource_not_authorized")
	require.EqualValues(t, 6, filteredPages.Load())
	require.EqualValues(t, 1, authWrites.Load())
	management, err := r.Read(context.Background(), "proxies", nil)
	require.NoError(t, err)
	require.Equal(t, "docker-or-unavailable", management["items"].([]map[string]any)[0]["id"], "management lists must not inherit upload-only filters")
	_, err = r.Read(context.Background(), "account-upload/options", url.Values{"available_for_binding": {"0"}})
	requireRemoteError(t, err, 400, "invalid_query")
}
