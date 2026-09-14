package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDedicatedAuditDoesNotBufferCredentialResponse(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/mailbox-management/accounts/1/credentials", nil)
	common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
	original := c.Writer
	require.Nil(t, beginAdminAudit(c))
	require.Same(t, original, c.Writer)
}
