package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/constant"
	"github.com/01121531/subandnew-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupControllerTestDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	previousSetup := constant.Setup
	dsn := fmt.Sprintf("file:setup-controller-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Setup{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	constant.Setup = false
	t.Cleanup(func() {
		model.DB = previousDB
		constant.Setup = previousSetup
		_ = sqlDB.Close()
	})
}

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/setup", PostSetup)
	return router
}

func setupRequest(router http.Handler, remoteAddr, token string) *httptest.ResponseRecorder {
	body := `{"username":"admin","password":"password123","confirmPassword":"password123"}`
	request := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remoteAddr
	if token != "" {
		request.Header.Set("X-Setup-Token", token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func setupSucceeded(t *testing.T, response *httptest.ResponseRecorder) bool {
	t.Helper()
	var payload struct {
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	return payload.Success
}

func TestPostSetupRequiresTokenForRemoteRequests(t *testing.T) {
	setupControllerTestDB(t)
	t.Setenv("SETUP_TOKEN", "one-time-setup-token")
	router := setupTestRouter()

	denied := setupRequest(router, "203.0.113.10:1234", "wrong-token")
	require.Equal(t, http.StatusForbidden, denied.Code)
	require.False(t, setupSucceeded(t, denied))

	allowed := setupRequest(router, "203.0.113.10:1234", "one-time-setup-token")
	require.Equal(t, http.StatusOK, allowed.Code)
	require.True(t, setupSucceeded(t, allowed))
	require.True(t, model.RootUserExists())
	require.NotNil(t, model.GetSetup())
}

func TestPostSetupWithoutTokenOnlyAllowsDirectLoopback(t *testing.T) {
	setupControllerTestDB(t)
	t.Setenv("SETUP_TOKEN", "")
	router := setupTestRouter()

	forwarded := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"username":"admin","password":"password123","confirmPassword":"password123"}`))
	forwarded.Header.Set("Content-Type", "application/json")
	forwarded.Header.Set("X-Forwarded-For", "203.0.113.10")
	forwarded.RemoteAddr = "127.0.0.1:1234"
	forwardedResponse := httptest.NewRecorder()
	router.ServeHTTP(forwardedResponse, forwarded)
	require.Equal(t, http.StatusForbidden, forwardedResponse.Code)

	allowed := setupRequest(router, "127.0.0.1:1234", "")
	require.True(t, setupSucceeded(t, allowed))
}

func TestPostSetupConcurrentRequestsCreateOneRoot(t *testing.T) {
	setupControllerTestDB(t)
	t.Setenv("SETUP_TOKEN", "one-time-setup-token")
	router := setupTestRouter()

	const requestCount = 4
	results := make(chan *httptest.ResponseRecorder, requestCount)
	var waitGroup sync.WaitGroup
	for index := 0; index < requestCount; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			results <- setupRequest(router, "203.0.113.10:1234", "one-time-setup-token")
		}()
	}
	waitGroup.Wait()
	close(results)

	successes := 0
	for response := range results {
		if setupSucceeded(t, response) {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	var rootCount int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("role = ?", common.RoleRootUser).Count(&rootCount).Error)
	require.Equal(t, int64(1), rootCount)
	var setupCount int64
	require.NoError(t, model.DB.Model(&model.Setup{}).Count(&setupCount).Error)
	require.Equal(t, int64(1), setupCount)
}
