package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTrustedProxiesDefaultsAndExplicitOverride(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	require.Contains(t, TrustedProxies(), "127.0.0.1/32")
	require.Contains(t, TrustedProxies(), "172.64.0.0/13")
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 192.168.1.1")
	require.Equal(t, []string{"10.0.0.0/8", "192.168.1.1"}, TrustedProxies())
}

func TestTrustedProxyChainResolvesClientWithoutTrustingSpoofedHeaders(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NoError(t, engine.SetTrustedProxies(TrustedProxies()))
	engine.GET("/ip", func(c *gin.Context) { c.String(http.StatusOK, c.ClientIP()) })

	tests := []struct {
		name, remote, forwarded, expected string
	}{
		{"cloudflare through loopback", "127.0.0.1:1234", "203.0.113.8, 172.68.10.2", "203.0.113.8"},
		{"direct client through loopback", "127.0.0.1:1234", "198.51.100.4, 192.0.2.20", "192.0.2.20"},
		{"untrusted direct peer", "192.0.2.30:1234", "203.0.113.99", "192.0.2.30"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/ip", nil)
			request.RemoteAddr = test.remote
			request.Header.Set("X-Forwarded-For", test.forwarded)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, test.expected, response.Body.String())
		})
	}
}
