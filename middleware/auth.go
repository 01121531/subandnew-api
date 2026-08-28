package middleware

import (
	"net/http"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/i18n"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func validUserInfo(username string, role int) bool {
	return strings.TrimSpace(username) != "" && common.IsValidateRole(role)
}

func authHelper(c *gin.Context, minRole int) {
	session := sessions.Default(c)
	username, usernameOK := session.Get("username").(string)
	role, roleOK := session.Get("role").(int)
	id, idOK := session.Get("id").(int)
	status, statusOK := session.Get("status").(int)

	if !usernameOK || !roleOK || !idOK || !statusOK {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthNotLoggedIn),
		})
		c.Abort()
		return
	}
	user, err := model.GetUserById(id, false)
	if err != nil || user == nil || user.Id != id {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthNotLoggedIn),
		})
		c.Abort()
		return
	}
	username = user.Username
	role = user.Role
	status = user.Status
	if status != common.UserStatusEnabled {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthUserBanned),
		})
		c.Abort()
		return
	}
	if role < minRole {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
		})
		c.Abort()
		return
	}
	if !validUserInfo(username, role) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthUserInfoInvalid),
		})
		c.Abort()
		return
	}

	c.Set("username", username)
	c.Set("role", role)
	c.Set("id", id)

	var auditWriter *auditResponseWriter
	if minRole >= common.RoleAdminUser {
		auditWriter = beginAdminAudit(c)
	}
	c.Next()
	finishAdminAudit(c, auditWriter)
}

func UserAuth() gin.HandlerFunc {
	return func(c *gin.Context) { authHelper(c, common.RoleCommonUser) }
}

func AdminAuth() gin.HandlerFunc {
	return func(c *gin.Context) { authHelper(c, common.RoleAdminUser) }
}

func RootAuth() gin.HandlerFunc {
	return func(c *gin.Context) { authHelper(c, common.RoleRootUser) }
}

func RequirePermission(permission authz.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		if authz.Can(c.GetInt("id"), c.GetInt("role"), permission) {
			c.Next()
			return
		}
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege),
		})
		c.Abort()
	}
}
