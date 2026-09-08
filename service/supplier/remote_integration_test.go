package supplier

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRemoteServiceComposition(t *testing.T) {
	t.Setenv("MANAGED_INSTANCE_SECRET_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	t.Setenv("MANAGED_INSTANCE_SECRET_KEY_VERSION", "remote-integration-v1")
	redis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = redis })
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "supplier-remote.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.ManagedInstance{}, &model.Supplier{}, &model.SupplierBinding{}, &model.SupplierSession{}, &model.SupplierOAuthFlow{}, &model.SupplierAudit{}))

	var proxyWrites, exchanges atomic.Int64
	var uncertainExchange atomic.Bool
	var payloadMu sync.Mutex
	var frozenPayload map[string]any
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		for key, path := range remoteOptionPaths {
			if req.URL.Path == path {
				json.NewEncoder(w).Encode(map[string]any{key: []any{map[string]any{"id": key + "-id", "name": key, "is_owner": true, "host": "proxy.example", "port": "8080", "password": "DO NOT EXPOSE"}}, "total": 1})
				return true
			}
		}
		if req.URL.Path == remoteAccountsPath+"/auth-url" || req.URL.Path == remoteAccountsPath+"/exchange" {
			var payload map[string]any
			require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
			require.Equal(t, false, payload["overwrite_existing"])
			require.Equal(t, false, payload["overwrite"])
			require.Equal(t, "anthropic", payload["provider"])
			require.Equal(t, "login", payload["oauth_flow"])
			require.Equal(t, "native", payload["inference_backend"])
			if strings.HasSuffix(req.URL.Path, "/auth-url") {
				payloadMu.Lock()
				frozenPayload = payload
				payloadMu.Unlock()
				io.WriteString(w, `{"data":{"url":"https://claude.ai/oauth/authorize?code=true&state=synthetic-frozen-state&redirect_uri=https%3A%2F%2Fplatform.claude.com%2Foauth%2Fcode%2Fcallback","state":"synthetic-frozen-state"}}`)
			} else {
				exchanges.Add(1)
				require.Equal(t, "synthetic-code#synthetic-frozen-state", payload["code"])
				require.Equal(t, "synthetic-frozen-state", payload["pending_state"])
				delete(payload, "code")
				delete(payload, "pending_state")
				payloadMu.Lock()
				require.Equal(t, frozenPayload, payload)
				payloadMu.Unlock()
				var flow model.SupplierOAuthFlow
				require.NoError(t, db.Order("id DESC").First(&flow).Error)
				require.Greater(t, flow.ConsumedAt, int64(0))
				require.Empty(t, flow.Ciphertext, "state must be consumed before upstream exchange")
				if uncertainExchange.Load() {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
				} else {
					io.WriteString(w, `{"data":{"id":"new-account","access_token":"DO NOT EXPOSE"}}`)
				}
			}
			return true
		}
		if strings.HasPrefix(req.URL.Path, remoteProxiesPath+"/") {
			proxyWrites.Add(1)
			var payload map[string]any
			require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
			switch {
			case strings.HasSuffix(req.URL.Path, "/import"):
				require.Equal(t, map[string]any{"text": "http://synthetic:proxy-pass@proxy.example:8080"}, payload)
				io.WriteString(w, `{"imported":"1","failed":"0","errors":["DO NOT EXPOSE"]}`)
			case req.Method == http.MethodPatch:
				require.Equal(t, map[string]any{"status": "active"}, payload)
				io.WriteString(w, `{}`)
			case req.Method == http.MethodDelete:
				require.Empty(t, payload)
				w.WriteHeader(http.StatusNoContent)
			default:
				require.Empty(t, payload)
				io.WriteString(w, `{"ok":true,"latency_ms":"18","password":"DO NOT EXPOSE"}`)
			}
			return true
		}
		return false
	})
	instance := model.ManagedInstance{Name: "Mock Gateway", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: f.server.URL, TLSVerify: true}
	require.NoError(t, db.Create(&instance).Error)
	svc := New(db)
	enabled := true
	localPassword := "Synthetic-local-login-123"
	local, err := svc.Save(0, SupplierInput{Name: "Local supplier", Username: "local-vendor", Password: localPassword, ManageProxies: &enabled, UploadAccounts: &enabled})
	require.NoError(t, err)
	ctx := context.Background()
	binding, err := svc.SaveBinding(ctx, local.ID, 0, BindingInput{InstanceID: instance.Id, Identifier: "synthetic-vendor", Password: remoteTestPassword})
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", binding.RemoteUserID)
	require.NotContains(t, binding.Ciphertext, remoteTestPassword)
	principal, rawSession, err := svc.Login(ctx, local.Username, localPassword, "127.0.0.1")
	require.NoError(t, err)
	principal, err = svc.Authenticate(rawSession)
	require.NoError(t, err)

	result, err := svc.ProxyWrite(ctx, principal, binding.ID, http.MethodPost, "", "", map[string]any{"text": "http://synthetic:proxy-pass@proxy.example:8080", "status": ""})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"imported": float64(1), "failed": float64(0)}, result)
	result, err = svc.ProxyWrite(ctx, principal, binding.ID, http.MethodPost, "proxies-id", "test", map[string]any{"text": "", "status": ""})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"ok": true, "latency_ms": float64(18)}, result)
	_, err = svc.ProxyWrite(ctx, principal, binding.ID, http.MethodPatch, "proxies-id", "", map[string]any{"status": "enabled", "text": ""})
	require.NoError(t, err)
	_, err = svc.ProxyWrite(ctx, principal, binding.ID, http.MethodDelete, "proxies-id", "", map[string]any{"text": "", "status": ""})
	require.NoError(t, err)
	require.EqualValues(t, 4, proxyWrites.Load())
	options, err := svc.Read(ctx, principal, binding.ID, "account-upload/options", nil, true)
	require.NoError(t, err)
	encodedOptions, err := json.Marshal(options)
	require.NoError(t, err)
	require.Contains(t, string(encodedOptions), `"host":"proxy.example"`)
	require.Contains(t, string(encodedOptions), `"port":8080`)
	require.NotContains(t, string(encodedOptions), "DO NOT EXPOSE")

	for _, manual := range []bool{false, true} {
		input := UploadInput{BindingID: binding.ID, Name: "Frozen account", ProxyMode: "direct", MaxTPM: 1000000000}
		if manual {
			input.ProxyMode, input.ProxyID = "manual", "proxies-id"
			input.GroupIDs, input.PolicyID, input.TemplateID = []string{"groups-id"}, "policies-id", "templates-id"
		}
		started, err := svc.StartUpload(ctx, principal, input)
		require.NoError(t, err)
		require.NotContains(t, started, "state")
		flowToken, ok := started["flow_id"].(string)
		require.True(t, ok)
		var stored model.SupplierOAuthFlow
		require.NoError(t, db.Order("id DESC").First(&stored).Error)
		require.NotEmpty(t, stored.Ciphertext)
		require.NotContains(t, stored.Ciphertext, "synthetic-frozen-state")
		require.NotContains(t, stored.Ciphertext, input.Name)
		input.Name = "Modified after freezing"
		if manual {
			input.GroupIDs[0] = "unauthorized-after-freezing"
		}
		before := exchanges.Load()
		uncertainExchange.Store(manual)
		result, err = svc.Exchange(ctx, principal, flowToken, "https://platform.claude.com/oauth/code/callback?code=synthetic-code&state=synthetic-frozen-state")
		if manual {
			requireRemoteError(t, err, 502, "upstream_unavailable")
		} else {
			require.NoError(t, err)
			require.Equal(t, map[string]any{"completed": true}, result)
		}
		require.Equal(t, before+1, exchanges.Load())
		_, err = svc.Exchange(ctx, principal, flowToken, "synthetic-code#synthetic-frozen-state")
		status, _ := HTTPError(err)
		require.Equal(t, 409, status)
		require.Equal(t, before+1, exchanges.Load(), "consumed exchanges must never be replayed")
	}
	// Binding verification and the saved binding intentionally have distinct
	// remote namespaces; all subsequent portal writes share only the latter.
	require.EqualValues(t, 2, f.logins.Load())
	require.EqualValues(t, 2, f.profiles.Load())
}
