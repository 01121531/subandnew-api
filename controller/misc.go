package controller

import (
	"net/http"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/constant"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/oauth"
	"github.com/01121531/subandnew-api/setting/console_setting"
	"github.com/01121531/subandnew-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

func TestStatus(c *gin.Context) {
	if err := model.PingDB(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "database connection failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "Server is running"})
}

func GetStatus(c *gin.Context) {
	console := console_setting.GetConsoleSetting()
	passkey := system_setting.GetPasskeySettings()
	legal := system_setting.GetLegalSettings()
	data := gin.H{
		"version": common.Version, "start_time": common.StartTime, "setup": constant.Setup,
		"system_name": common.SystemName, "logo": common.Logo, "footer_html": common.Footer,
		"server_address": system_setting.ServerAddress, "password_login_enabled": common.PasswordLoginEnabled,
		"email_verification": common.EmailVerificationEnabled,
		"github_oauth":       common.GitHubOAuthEnabled, "github_client_id": common.GitHubClientId,
		"discord_oauth": system_setting.GetDiscordSettings().Enabled, "discord_client_id": system_setting.GetDiscordSettings().ClientId,
		"linuxdo_oauth": common.LinuxDOOAuthEnabled, "linuxdo_client_id": common.LinuxDOClientId,
		"linuxdo_minimum_trust_level": common.LinuxDOMinimumTrustLevel,
		"telegram_oauth":              common.TelegramOAuthEnabled, "telegram_bot_name": common.TelegramBotName,
		"wechat_login": common.WeChatAuthEnabled, "wechat_qrcode": common.WeChatAccountQRCodeImageURL,
		"turnstile_check": common.TurnstileCheckEnabled, "turnstile_site_key": common.TurnstileSiteKey,
		"oidc_enabled":                system_setting.GetOIDCSettings().Enabled,
		"oidc_client_id":              system_setting.GetOIDCSettings().ClientId,
		"oidc_authorization_endpoint": system_setting.GetOIDCSettings().AuthorizationEndpoint,
		"passkey_login":               passkey.Enabled, "passkey_display_name": passkey.RPDisplayName,
		"passkey_rp_id": passkey.RPID, "passkey_origins": passkey.Origins,
		"passkey_allow_insecure":    passkey.AllowInsecureOrigin,
		"passkey_user_verification": passkey.UserVerification, "passkey_attachment": passkey.AttachmentPreference,
		"announcements_enabled":  console.AnnouncementsEnabled,
		"user_agreement_enabled": legal.UserAgreement != "", "privacy_policy_enabled": legal.PrivacyPolicy != "",
		"docs_link": "https://github.com/01121531/subandnew-api",
	}
	if console.AnnouncementsEnabled {
		data["announcements"] = console_setting.GetAnnouncements()
	}

	providers := oauth.GetEnabledCustomProviders()
	if len(providers) > 0 {
		items := make([]gin.H, 0, len(providers))
		for _, provider := range providers {
			config := provider.GetConfig()
			items = append(items, gin.H{
				"id": config.Id, "name": config.Name, "slug": config.Slug, "icon": config.Icon,
				"client_id": config.ClientId, "authorization_endpoint": config.AuthorizationEndpoint, "scopes": config.Scopes,
			})
		}
		data["custom_oauth_providers"] = items
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}
