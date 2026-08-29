package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPortalSummaryOnlyExposesAuthorizedMetrics(t *testing.T) {
	result := &managedaccount.Result{Summary: managedaccount.Summary{Total: 8, Available: 5, Unavailable: 3,
		Requests: 120, Tokens: 240, Amounts: map[string]float64{"USD": 12.5}}}
	limited := portalSummary(result, []string{"name"})
	require.Equal(t, 8, limited["total"])
	require.NotContains(t, limited, "available")
	require.NotContains(t, limited, "requests")
	require.NotContains(t, limited, "tokens")
	require.NotContains(t, limited, "amounts")

	allowed := portalSummary(result, []string{"available", "requests", "amount"})
	require.Equal(t, 5, allowed["available"])
	require.Equal(t, float64(120), allowed["requests"])
	require.Equal(t, map[string]float64{"USD": 12.5}, allowed["amounts"])
	require.NotContains(t, allowed, "tokens")
}

func TestPortalSessionProbeReturnsLoggedOutStateWithoutCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/open-portal/v1/account-data/:slug/session", GetAccountDataPortalSession)
	engine.POST("/open-portal/v1/account-data/:slug/query", QueryAccountDataPortal)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/open-portal/v1/account-data/example/session", nil)
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Authenticated bool `json:"authenticated"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.False(t, response.Data.Authenticated)

	protectedRecorder := httptest.NewRecorder()
	protectedRequest := httptest.NewRequest(http.MethodPost, "/open-portal/v1/account-data/example/query", strings.NewReader(`{}`))
	protectedRequest.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(protectedRecorder, protectedRequest)
	require.Equal(t, http.StatusUnauthorized, protectedRecorder.Code)
}

func TestPortalSessionProbeClearsInvalidCookie(t *testing.T) {
	previous := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ManagedAccountAPI{}))
	model.DB = db
	t.Cleanup(func() { model.DB = previous })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/open-portal/v1/account-data/:slug/session", GetAccountDataPortalSession)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/open-portal/v1/account-data/missing/session", nil)
	request.AddCookie(&http.Cookie{Name: portalCookieName, Value: "invalid"})
	engine.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"success":true,"message":"","data":{"authenticated":false}}`, recorder.Body.String())
	setCookie := recorder.Header().Get("Set-Cookie")
	require.Contains(t, setCookie, portalCookieName+"=")
	require.Contains(t, setCookie, "Max-Age=0")
	require.Contains(t, setCookie, "HttpOnly")
}
