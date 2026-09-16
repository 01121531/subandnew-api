package router

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMailboxRoutesAndAnonymousLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldLimit, oldRate := constant.AnonymousRequestBodyLimitKB, common.GlobalApiRateLimitEnable
	constant.AnonymousRequestBodyLimitKB = 1
	common.GlobalApiRateLimitEnable = false
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = oldLimit; common.GlobalApiRateLimitEnable = oldRate })
	r := gin.New()
	registerMailboxRoutes(r, r.Group("/api"))
	routes := map[string]bool{}
	for _, route := range r.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, suffix := range []string{"/work-summary", "/work-accounts", "/work-accounts/:account_id/history"} {
		require.True(t, routes["GET /api/mailbox-management/operators/:id"+suffix])
		require.False(t, routes["GET /mailbox-api/v1/operators/:id"+suffix])
	}
	for _, route := range []string{"GET /api/mailbox-management/accounts", "POST /api/mailbox-management/imports/preview", "POST /api/mailbox-management/imports", "POST /api/mailbox-management/assignments", "POST /api/mailbox-management/operators/:id/password", "POST /api/mailbox-management/submissions/:id/review", "GET /api/mailbox-management/attachments/:id", "GET /api/mailbox-management/audits", "GET /mailbox-api/v1/auth/session", "POST /mailbox-api/v1/accounts/:id/credentials", "POST /mailbox-api/v1/assignments/:id/attachments", "POST /mailbox-api/v1/assignments/:id/submit", "GET /mailbox-api/v1/attachments/:id"} {
		require.True(t, routes[route], route)
	}
	req := httptest.NewRequest("POST", "/mailbox-api/v1/auth/login", strings.NewReader(strings.Repeat("x", 1025)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 413, w.Code)
}
