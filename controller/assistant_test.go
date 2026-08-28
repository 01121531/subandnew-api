package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/channel/wechatilink"
	"github.com/01121531/subandnew-api/service/assistant/channelservice"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAssistantErrorMapsChannelUpstreamFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		kind       wechatilink.ErrorKind
		statusCode int
		message    string
	}{
		{name: "invalid", kind: wechatilink.ErrorKindInvalid, statusCode: http.StatusBadRequest, message: "invalid_assistant_channel_request"},
		{name: "rate limited", kind: wechatilink.ErrorKindRateLimit, statusCode: http.StatusTooManyRequests, message: "assistant_channel_rate_limited"},
		{name: "timeout", kind: wechatilink.ErrorKindTimeout, statusCode: http.StatusGatewayTimeout, message: "assistant_channel_timeout"},
		{name: "upstream http", kind: wechatilink.ErrorKindHTTP, statusCode: http.StatusBadGateway, message: "assistant_channel_upstream_unavailable"},
		{name: "expired", kind: wechatilink.ErrorKindSessionExpired, statusCode: http.StatusGone, message: "assistant_channel_login_expired"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			assistantError(context, &wechatilink.Error{Operation: "test", Kind: test.kind})
			require.Equal(t, test.statusCode, recorder.Code)
			require.True(t, strings.Contains(recorder.Body.String(), test.message), recorder.Body.String())
		})
	}
}

func TestAssistantErrorMapsCompletedLoginConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	assistantError(context, channelservice.ErrLoginAlreadyComplete)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), "assistant_channel_login_already_completed")
}

func TestAssistantListResponsesUseEmptyArrays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.AssistantRun{},
		&model.AssistantIdentity{},
		&model.AssistantIdentityInstanceScope{},
		&model.AssistantSetting{},
		&model.ManagedInstance{},
	))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })

	runRecorder := httptest.NewRecorder()
	runContext, _ := gin.CreateTestContext(runRecorder)
	runContext.Request = httptest.NewRequest(http.MethodGet, "/api/assistant/runs", nil)
	ListAssistantRuns(runContext)
	require.Equal(t, http.StatusOK, runRecorder.Code)
	require.Contains(t, runRecorder.Body.String(), `"items":[]`)

	user := model.User{Username: "assistant-user", Password: "hash", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	identity := model.AssistantIdentity{
		ChannelID: 1, ExternalUserID: "wx-user", UserID: user.Id,
		Status: model.AssistantIdentityStatusActive, AllowedInstanceScope: model.AssistantInstanceScopeAll,
	}
	require.NoError(t, db.Create(&identity).Error)
	identityRecorder := httptest.NewRecorder()
	identityContext, _ := gin.CreateTestContext(identityRecorder)
	identityContext.Request = httptest.NewRequest(http.MethodGet, "/api/assistant/identities", nil)
	ListAssistantIdentities(identityContext)
	require.Equal(t, http.StatusOK, identityRecorder.Code)
	require.Contains(t, identityRecorder.Body.String(), `"instance_ids":[]`)
}
