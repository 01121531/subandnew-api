package supplier

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoteAccountDiagnosticsAndRuntime(t *testing.T) {
	f := newRemoteFixture(t, func(w http.ResponseWriter, req *http.Request) bool {
		if req.URL.Path != remoteAccountsPath {
			return false
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"accounts": []any{
			map[string]any{"id": "account-1", "status": "active", "health_status": "error", "failure_kind": "account_proxy_failure", "last_error": "Bound outbound proxy is unhealthy", "stats": map[string]any{"cooldown": true, "cooldown_reason": "account_proxy_failure: upstream_header_timeout:240s", "cooldown_remaining_seconds": "5", "rpm": "12", "tpm": 1200, "concurrent": 2, "active_sessions": "300"}, "max_rpm": 1000, "max_tpm": "50000000", "max_concurrent": 400, "max_sessions": 300, "access_token": "do-not-return"},
			map[string]any{"id": "account-2", "status": "active", "stats": map[string]any{"cooldown": false, "cooldown_remaining_seconds": 0, "rpm": 0, "active_sessions": 0}, "max_sessions": 0},
			map[string]any{"id": "account-3", "last_error": "upstream rejected: " + remoteTestToken},
			map[string]any{"id": "account-4", "last_error": "HTTP 401 bearer abcdefghijklmnop password=synthetic123 https://user:secret@example.test/private 127.0.0.1"},
		}, "total": 4}))
		return true
	})
	r := f.client(t.Name(), "9007199254740993")
	result, err := r.Read(context.Background(), "accounts", nil)
	require.NoError(t, err)
	rows := result["items"].([]map[string]any)
	require.Equal(t, "active", rows[0]["status"])
	require.Equal(t, "error", rows[0]["health_status"])
	require.Equal(t, "account_proxy_failure", rows[0]["failure_kind"])
	require.Equal(t, "Bound outbound proxy is unhealthy", rows[0]["last_error"])
	require.Equal(t, "account_proxy_failure: upstream_header_timeout:240s", rows[0]["cooldown_reason"])
	require.Equal(t, true, rows[0]["cooldown"])
	for field, expected := range map[string]float64{"rpm": 12, "tpm": 1200, "concurrent": 2, "active_sessions": 300, "max_rpm": 1000, "max_tpm": 50000000, "max_concurrent": 400, "max_sessions": 300, "cooldown_remaining_seconds": 5} {
		require.Equal(t, expected, rows[0][field], field)
	}
	require.NotContains(t, rows[0], "access_token")
	require.Equal(t, false, rows[1]["cooldown"])
	for _, field := range []string{"cooldown_remaining_seconds", "rpm", "active_sessions", "max_sessions"} {
		require.Equal(t, float64(0), rows[1][field])
	}
	require.Nil(t, rows[1]["tpm"])
	require.Nil(t, rows[1]["last_error"])
	require.Nil(t, rows[2]["last_error"])
	text := rows[3]["last_error"].(string)
	require.Contains(t, text, "HTTP 401")
	for _, secret := range []string{"abcdefghijklmnop", "synthetic123", "example.test", "127.0.0.1"} {
		require.NotContains(t, text, secret)
	}
}

func TestRemoteAccountDiagnosticMissingMalformedAndLimits(t *testing.T) {
	require.Equal(t, "account disabled", remoteDiagnosticText(`{"error":{"message":"account disabled","token":"private"},"request":"private"}`))
	for _, value := range []any{nil, "", "  ", map[string]any{"message": "raw"}, `{"password":"private"}`, `[{"token":"private"}]`} {
		require.Nil(t, remoteDiagnosticText(value))
	}
	for _, seconds := range []any{-1, "NaN", "Infinity", 1.5, "invalid", nil} {
		out := map[string]any{}
		remoteAccountDiagnostics(map[string]any{"stats": map[string]any{"cooldown_remaining_seconds": seconds, "rpm": seconds}}, out)
		require.Nil(t, out["cooldown_remaining_seconds"])
		require.Nil(t, out["rpm"])
		require.Nil(t, out["cooldown"])
	}
	require.LessOrEqual(t, len([]rune(remoteDiagnosticText(strings.Repeat("异常", 500)).(string))), 303)
}
