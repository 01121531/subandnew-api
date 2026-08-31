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

func TestOpenAccountDataRoutesApplyAnonymousBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousLimit := constant.AnonymousRequestBodyLimitKB
	previousRateLimit := common.GlobalApiRateLimitEnable
	constant.AnonymousRequestBodyLimitKB = 1
	common.GlobalApiRateLimitEnable = false
	t.Cleanup(func() {
		constant.AnonymousRequestBodyLimitKB = previousLimit
		common.GlobalApiRateLimitEnable = previousRateLimit
	})
	engine := gin.New()
	registerAccountDataAPIRoutes(engine, engine.Group("/api"))
	request := httptest.NewRequest(http.MethodPost, "/open-portal/v1/account-data/example/login", strings.NewReader(strings.Repeat("x", 1025)))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}
