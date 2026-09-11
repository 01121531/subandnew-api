package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSupplierRoutesAndAnonymousLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldLimit, oldRate := constant.AnonymousRequestBodyLimitKB, common.GlobalApiRateLimitEnable
	constant.AnonymousRequestBodyLimitKB = 1
	common.GlobalApiRateLimitEnable = false
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = oldLimit; common.GlobalApiRateLimitEnable = oldRate })
	r := gin.New()
	registerSupplierRoutes(r, r.Group("/api"))
	routes := map[string]bool{}
	for _, route := range r.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	require.True(t, routes["GET /api/suppliers/default-policy"])
	require.True(t, routes["PUT /api/suppliers/default-policy"])
	for _, route := range []string{"GET /api/suppliers", "POST /api/suppliers/:id/password", "GET /api/suppliers/:id/audits", "GET /supplier-api/v1/auth/session", "POST /supplier-api/v1/account-upload/exchange", "POST /supplier-api/v1/account-upload/import-rt", "POST /supplier-api/v1/account-upload/import-sk", "PATCH /supplier-api/v1/proxies/:id"} {
		require.True(t, routes[route], route)
	}
	req := httptest.NewRequest(http.MethodPost, "/supplier-api/v1/auth/login", strings.NewReader(strings.Repeat("x", 1025)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 413, w.Code)
}
