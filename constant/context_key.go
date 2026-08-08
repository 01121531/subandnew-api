package constant

type ContextKey string

const (
	ContextKeyUserSetting ContextKey = "user_setting"
	ContextKeyLanguage    ContextKey = "language"
	ContextKeyAuditLogged ContextKey = "audit_logged"
)
