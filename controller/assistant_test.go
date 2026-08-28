package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/service/assistant/channel/wechatilink"
	"github.com/01121531/subandnew-api/service/assistant/channelservice"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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
