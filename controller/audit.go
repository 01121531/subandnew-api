package controller

import (
	"fmt"
	"os"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/constant"
	"github.com/01121531/subandnew-api/model"
	"github.com/gin-gonic/gin"
)

var auditContentTemplates = map[string]string{
	"user.create":                 "Created user ${username} (role ${role})",
	"user.update":                 "Updated user ${username} (ID: ${id})",
	"user.delete":                 "Deleted user ${username} (ID: ${id})",
	"user.manage":                 "Performed ${action} on user ${username} (ID: ${id})",
	"user.2fa_disable":            "Force-disabled two-factor authentication for the user",
	"user.passkey_register":       "Registered a passkey",
	"user.passkey_delete":         "Deleted a passkey",
	"user.reset_passkey":          "Reset the user passkey",
	"option.update":               "Updated system setting ${key}",
	"system_update.start":         "Started system update ${task_id} from ${from_version} to ${target_version}",
	"account_data_api.create":     "Created account data API authorization ${name} (ID: ${id})",
	"account_data_api.update":     "Updated account data API authorization ${name} (ID: ${id})",
	"account_data_api.delete":     "Deleted account data API authorization (ID: ${id})",
	"account_data_api.key_create": "Created a key for account data API authorization (ID: ${id})",
	"account_data_api.key_revoke": "Revoked key ${key_id} for account data API authorization (ID: ${id})",
}

func auditContentEN(action string, params map[string]interface{}) string {
	tmpl, ok := auditContentTemplates[action]
	if !ok {
		return action
	}
	return os.Expand(tmpl, func(key string) string { return fmt.Sprint(params[key]) })
}

func auditOperatorInfo(c *gin.Context) map[string]interface{} {
	return map[string]interface{}{
		"admin_id":       c.GetInt("id"),
		"admin_username": c.GetString("username"),
		"admin_role":     c.GetInt("role"),
		"auth_method":    auditAuthMethod(c),
	}
}

func auditAuthMethod(c *gin.Context) string {
	return "session"
}

func markAuditLogged(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
}

func recordManageAudit(c *gin.Context, action string, params map[string]interface{}) {
	recordManageAuditFor(c, c.GetInt("id"), action, params)
}

func recordManageAuditFor(c *gin.Context, targetUserID int, action string, params map[string]interface{}) {
	if params == nil {
		params = map[string]interface{}{}
	}
	operatorUserID := c.GetInt("id")
	if _, ok := params["target_user_id"]; !ok && targetUserID > 0 && targetUserID != operatorUserID {
		params["target_user_id"] = targetUserID
	}
	model.RecordOperationAuditLog(operatorUserID, auditContentEN(action, params), c.ClientIP(), action, params, auditOperatorInfo(c), nil)
	markAuditLogged(c)
}

func recordUserSecurityAudit(c *gin.Context, userID int, action string, params map[string]interface{}) {
	model.RecordOperationAuditLog(userID, auditContentEN(action, params), c.ClientIP(), action, params, nil, nil)
}
