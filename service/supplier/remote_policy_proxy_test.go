package supplier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoteProxyOwnershipWireFormats(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		owner        any
		reason       string
	}{
		{"bool-true", `"is_owner":true`, true, ""},
		{"bool-false", `"is_owner":false`, false, "proxy_not_owned"},
		{"number-one", `"is_owner":1`, true, ""},
		{"number-zero", `"is_owner":0`, false, "proxy_not_owned"},
		{"string-one", `"is_owner":"1"`, true, ""},
		{"string-zero", `"is_owner":"0"`, false, "proxy_not_owned"},
		{"string-true", `"is_owner":" TRUE "`, true, ""},
		{"string-false", `"is_owner":"false"`, false, "proxy_not_owned"},
		{"owner-id-number", `"owner_user_id":9007199254740993`, true, ""},
		{"owner-id-string", `"is_owner":null,"owner_user_id":"9007199254740993"`, true, ""},
		{"foreign-id", `"owner_user_id":"other"`, false, "proxy_not_owned"},
		{"explicit-denial", `"is_owner":false,"owner_user_id":"9007199254740993"`, false, "proxy_not_owned"},
		{"missing", `"name":"unknown"`, nil, "proxy_ownership_unknown"},
		{"null", `"is_owner":null`, nil, "proxy_ownership_unknown"},
		{"malformed", `"is_owner":"yes","owner_user_id":"9007199254740993"`, nil, "proxy_ownership_unknown"},
		{"object", `"is_owner":{}`, nil, "proxy_ownership_unknown"},
		{"unexpected-number", `"is_owner":2`, nil, "proxy_ownership_unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var writes atomic.Int64
			f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
				if req.URL.Path == remoteProxiesPath {
					fmt.Fprintf(w, `{"proxies":[{"id":"p1",%s}],"total":1}`, tc.fields)
					return true
				}
				if req.URL.Path == remoteProxiesPath+"/p1" {
					writes.Add(1)
					require.Equal(t, "PATCH", req.Method)
					var body map[string]any
					require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
					require.Equal(t, map[string]any{"status": "disabled"}, body)
					io.WriteString(w, `{"ok":true}`)
					return true
				}
				return false
			})
			r := f.client(t.Name(), "9007199254740993")
			data, err := r.Read(context.Background(), "proxies", nil)
			require.NoError(t, err)
			row := data["items"].([]map[string]any)[0]
			require.Equal(t, tc.owner, row["is_owner"])
			require.Equal(t, tc.owner == true, row["can_update_status"])
			require.Equal(t, tc.reason, row["status_update_reason"])
			require.NotContains(t, row, "owner_user_id")
			_, err = r.Write(context.Background(), "PATCH", "proxies/p1", map[string]any{"status": "disabled"})
			if tc.owner == true {
				require.NoError(t, err)
				require.EqualValues(t, 1, writes.Load())
			} else {
				requireRemoteError(t, err, 403, "")
				require.Zero(t, writes.Load())
			}
		})
	}
}

func TestRemoteProxyStatusRechecksOwnership(t *testing.T) {
	var owner atomic.Bool
	owner.Store(true)
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path != remoteProxiesPath {
			return false
		}
		fmt.Fprintf(w, `{"proxies":[{"id":"p1","is_owner":%t}],"total":1}`, owner.Load())
		return true
	})
	r := f.client(t.Name(), "")
	data, err := r.Read(context.Background(), "proxies", nil)
	require.NoError(t, err)
	require.Equal(t, true, data["items"].([]map[string]any)[0]["can_update_status"])
	owner.Store(false)
	_, err = r.Write(context.Background(), "PATCH", "proxies/p1", map[string]any{"status": "disabled"})
	requireRemoteError(t, err, 403, "resource_not_owned")
}

func TestRemotePolicyLimitsWhitelist(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		expected map[string]any
	}{
		{`{"max_rpm":"1000","max_tpm":50000000,"max_concurrent":"400","max_sessions":0,"secret":"hidden"}`, map[string]any{"max_rpm": float64(1000), "max_tpm": float64(50000000), "max_concurrent": float64(400), "max_sessions": float64(0)}},
		{`{"max_rpm":null,"max_tpm":"","max_concurrent":false}`, map[string]any{"max_rpm": nil, "max_tpm": nil, "max_concurrent": nil, "max_sessions": nil}},
		{`{"max_rpm":-1,"max_tpm":"NaN","max_concurrent":1.5,"max_sessions":100001}`, map[string]any{"max_rpm": nil, "max_tpm": nil, "max_concurrent": nil, "max_sessions": nil}},
	} {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(`{"id":"p1","name":"limits","password":"hidden","policy":`+tc.raw+`}`), &m))
		result := remotePolicy(m)
		require.Equal(t, tc.expected, result["policy"])
		require.Len(t, result, 3)
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "hidden")
	}
}
