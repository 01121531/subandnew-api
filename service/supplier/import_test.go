package supplier

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestSupplierSetupTokenFrozenAndLegacyDefault(t *testing.T) {
	for _, method := range []string{"", "login", "setup_token"} {
		t.Run("flow-"+method, func(t *testing.T) {
			f := newCoreFixture(t)
			s := f.supplier(t, "vendor", true)
			b := f.binding(t, s.ID, f.instance(t, 1).Id)
			p, _ := f.login(t, "vendor")
			in := coreUpload(b.ID)
			in.OAuthFlow = method
			flow, err := f.s.StartUpload(context.Background(), p, in)
			require.NoError(t, err)
			expected := method
			if expected == "" {
				expected = "login"
			}
			require.Equal(t, expected, f.r.body["oauth_flow"])
			_, err = f.s.Exchange(context.Background(), p, flow["flow_id"].(string), "code#test-state")
			require.NoError(t, err)
			require.Equal(t, expected, f.r.body["oauth_flow"])
			_, err = f.s.Exchange(context.Background(), p, flow["flow_id"].(string), "code#test-state")
			requireCoreError(t, err, 409, "")
		})
	}
}

func TestSupplierAccountImportPermissionValidationAndNoPersistence(t *testing.T) {
	f := newCoreFixture(t)
	s := f.supplier(t, "vendor", true)
	b := f.binding(t, s.ID, f.instance(t, 1).Id)
	p, _ := f.login(t, "vendor")
	in := ImportAccountInput{UploadInput: coreUpload(b.ID), RefreshToken: "synthetic-refresh", AccessToken: "synthetic-access"}
	_, err := f.s.ImportAccounts(context.Background(), p, "rt", in)
	require.NoError(t, err)
	require.Equal(t, "account-upload/import-rt", f.r.resource)
	var count int64
	require.NoError(t, f.db.Model(&model.SupplierOAuthFlow{}).Count(&count).Error)
	require.Zero(t, count)
	in.RefreshToken = ""
	in.AccessToken = ""
	in.SessionKeys = []string{"Key", "key", "Key"}
	_, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	require.NoError(t, err)
	require.Equal(t, []any{"Key", "key"}, f.r.body["session_keys"])
	in.SessionKeys = make([]string, 21)
	_, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	requireCoreError(t, err, 400, "")
	in.SessionKeys = []string{"bad\nkey"}
	_, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	requireCoreError(t, err, 400, "")
	in.SessionKeys = []string{"key"}
	in.OAuthFlow = "setup_token"
	_, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	requireCoreError(t, err, 400, "")
	in.OAuthFlow = ""
	other := f.supplier(t, "other", true)
	otherBinding := f.binding(t, other.ID, f.instance(t, 2).Id)
	in.BindingID = otherBinding.ID
	_, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	require.Error(t, err)
	in.BindingID = b.ID
	s.PolicyOverrides["upload_accounts"] = boolPtr(false)
	require.NoError(t, f.db.Model(s).Select("policy_overrides").Updates(s).Error)
	_, err = f.s.ImportAccounts(context.Background(), p, "sk", in)
	requireCoreError(t, err, 403, "")
}

func TestRemoteAccountImportRoutesAndProxies(t *testing.T) {
	for _, mode := range []string{"direct", "manual", "auto"} {
		t.Run(mode, func(t *testing.T) {
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path == remoteProxiesPath {
					io.WriteString(w, `{"proxies":[{"id":"p1"}],"total":1}`)
					return true
				}
				if !strings.HasPrefix(req.URL.Path, remoteAccountsPath+"/import-") {
					return false
				}
				var body map[string]any
				require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
				require.Equal(t, "native", body["inference_backend"])
				if strings.HasSuffix(req.URL.Path, "/import-rt") {
					require.Equal(t, "synthetic-rt", body["refresh_token"])
					require.Equal(t, false, body["overwrite_existing"])
					require.NotContains(t, body, "oauth_flow")
					io.WriteString(w, `{"id":"account1","refresh_token":"never-return"}`)
				} else {
					source := body["proxy_source"].(map[string]any)
					expected := mode
					if mode == "manual" {
						expected = "existing"
						require.Equal(t, []any{"p1"}, source["existing_ids"])
					}
					require.Equal(t, expected, source["mode"])
					require.Equal(t, "Test", body["name_prefix"])
					require.NotContains(t, body, "outbound_proxy_id")
					io.WriteString(w, `{"total":2,"ok":99,"results":[{"index":0,"ok":true,"token":"never-return"},{"index":1,"duplicate":true}]}`)
				}
				return true
			})
			r := f.client(t.Name(), "")
			for _, kind := range []string{"rt", "sk"} {
				body := map[string]any{"name": "Test", "outbound_proxy_mode": mode, "max_rpm": 0}
				if mode == "manual" {
					body["outbound_proxy_id"] = "p1"
				}
				if kind == "rt" {
					body["refresh_token"] = "synthetic-rt"
				} else {
					body["session_keys"] = []string{"synthetic-sk1", "synthetic-sk2"}
				}
				result, err := r.Write(context.Background(), "POST", "account-upload/import-"+kind, body)
				require.NoError(t, err)
				require.Equal(t, 1, result["ok"])
				encoded, _ := json.Marshal(result)
				require.NotContains(t, string(encoded), "never-return")
				require.NotContains(t, string(encoded), "synthetic-")
			}
		})
	}
}

func TestRemoteImportUnconfirmedAndNoReplay(t *testing.T) {
	for _, status := range []int{408, 429, 502, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int64
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path != remoteAccountsPath+"/import-rt" {
					return false
				}
				calls.Add(1)
				w.WriteHeader(status)
				io.WriteString(w, `not JSON secret`)
				return true
			})
			_, err := f.client(t.Name(), "").Write(context.Background(), "POST", "account-upload/import-rt", map[string]any{"name": "Test", "refresh_token": "synthetic-rt"})
			require.Error(t, err)
			require.EqualValues(t, 1, calls.Load())
			require.NotContains(t, err.Error(), "secret")
		})
	}
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path != remoteAccountsPath+"/import-rt" {
			return false
		}
		select {
		case <-req.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
		return true
	})
	r := f.client(t.Name()+"timeout", "")
	_, err := r.Verify(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = r.Write(ctx, "POST", "account-upload/import-rt", map[string]any{"name": "Test", "refresh_token": "synthetic-rt"})
	require.Error(t, err)
}

func TestRemoteBatchResultIntegrity(t *testing.T) {
	for _, tc := range []struct {
		raw         string
		ok, unknown int
	}{
		{`{"results":[{"index":0,"ok":true}]}`, 1, 1},
		{`{"results":[{"index":0,"ok":true},{"index":0,"ok":true}]}`, 0, 2},
		{`{"results":[{"index":0,"ok":true},{"index":99,"ok":true}]}`, 0, 2},
		{`{"total":3,"results":[{"index":0,"ok":true}]}`, 0, 2},
		{`{"ok":2}`, 0, 2},
		{`{"results":[{"index":0,"ok":"false","status":"imported"},{"index":1,"ok":true,"error":"secret"}]}`, 0, 2},
		{`{"results":[{"index":0,"ok":true,"duplicate":true},{"index":1,"ok":false,"status":"imported"}]}`, 0, 2},
		{`{"results":[{"index":0,"ok":false,"error":"proxy failed password=secret"},{"index":1,"status":"reauth_required"}]}`, 0, 0},
	} {
		var value any
		require.NoError(t, json.Unmarshal([]byte(tc.raw), &value))
		result := remoteImportResult(value, "import-sk", 2)
		require.Equal(t, tc.ok, result["ok"], tc.raw)
		require.Equal(t, tc.unknown, result["unknown"], tc.raw)
		raw, _ := json.Marshal(result)
		require.NotContains(t, string(raw), "secret")
	}
}

func TestRemoteRTResultAndOAuthModes(t *testing.T) {
	for _, tc := range []struct {
		raw, status string
	}{
		{`{"id":"account1","status":"active"}`, "imported"},
		{`{"duplicate":true}`, "duplicate"},
		{`{"id":"account1","ok":"false"}`, "unknown"},
		{`{"id":"account1","ok":false}`, "failed"},
		{`{"duplicate":true,"ok":true}`, "unknown"},
		{`{}`, "unknown"},
	} {
		var value any
		require.NoError(t, json.Unmarshal([]byte(tc.raw), &value))
		result := remoteImportResult(value, "import-rt", 1)
		require.Equal(t, tc.status, result["results"].([]map[string]any)[0]["status"])
	}
	for _, flow := range []string{"login", "setup_token"} {
		body, err := remoteUploadBody(map[string]any{"oauth_flow": flow}, false)
		require.NoError(t, err)
		require.Equal(t, flow, body["oauth_flow"])
	}
}

func TestRemoteAccountImportAuthRecovery(t *testing.T) {
	for _, code := range []int{401, 403} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			var calls atomic.Int64
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path != remoteAccountsPath+"/import-rt" {
					return false
				}
				if calls.Add(1) == 1 {
					w.WriteHeader(code)
					return true
				}
				io.WriteString(w, `{"id":"account1","status":"active"}`)
				return true
			})
			result, err := f.client(t.Name(), "").Write(context.Background(), "POST", "account-upload/import-rt", map[string]any{"name": "Test", "refresh_token": "synthetic-rt"})
			require.NoError(t, err)
			require.Equal(t, 1, result["ok"])
			require.EqualValues(t, 2, calls.Load())
			require.EqualValues(t, 1, f.refreshes.Load())
			require.EqualValues(t, 1, f.logins.Load())
		})
	}
}
