package constant

type ContextKey string

const (
	ContextKeyTokenGroup           ContextKey = "token_group"
	ContextKeyTokenCrossGroupRetry ContextKey = "token_cross_group_retry"

	ContextKeyUserSetting ContextKey = "user_setting"
	ContextKeyUserQuota   ContextKey = "user_quota"
	ContextKeyUserStatus  ContextKey = "user_status"
	ContextKeyUserEmail   ContextKey = "user_email"
	ContextKeyUserGroup   ContextKey = "user_group"
	ContextKeyUsingGroup  ContextKey = "group"
	ContextKeyUserName    ContextKey = "username"

	ContextKeyLanguage    ContextKey = "language"
	ContextKeyAuditLogged ContextKey = "audit_logged"
)
