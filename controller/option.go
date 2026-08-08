package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

func GetOptions(c *gin.Context) {
	options := make([]*model.Option, 0)
	common.OptionMapRWMutex.RLock()
	for key, value := range common.OptionMap {
		lower := strings.ToLower(key)
		if strings.HasSuffix(lower, "token") || strings.HasSuffix(lower, "secret") || strings.HasSuffix(lower, "key") || strings.HasSuffix(lower, "api_key") {
			continue
		}
		options = append(options, &model.Option{Key: key, Value: common.Interface2String(value)})
	}
	common.OptionMapRWMutex.RUnlock()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": options})
}

type OptionUpdateRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func UpdateOption(c *gin.Context) {
	var request OptionUpdateRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || !model.IsControlPlaneOptionKey(request.Key) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "unsupported control-plane option"})
		return
	}
	value := fmt.Sprintf("%v", request.Value)

	switch request.Key {
	case "GitHubOAuthEnabled":
		if value == "true" && common.GitHubClientId == "" {
			common.ApiErrorMsg(c, "GitHub Client ID must be configured first")
			return
		}
	case "LinuxDOOAuthEnabled":
		if value == "true" && common.LinuxDOClientId == "" {
			common.ApiErrorMsg(c, "LinuxDO Client ID must be configured first")
			return
		}
	case "TelegramOAuthEnabled":
		if value == "true" && common.TelegramBotToken == "" {
			common.ApiErrorMsg(c, "Telegram bot token must be configured first")
			return
		}
	case "WeChatAuthEnabled":
		if value == "true" && common.WeChatServerAddress == "" {
			common.ApiErrorMsg(c, "WeChat server address must be configured first")
			return
		}
	case "TurnstileCheckEnabled":
		if value == "true" && common.TurnstileSiteKey == "" {
			common.ApiErrorMsg(c, "Turnstile site key must be configured first")
			return
		}
	case "discord.enabled":
		if value == "true" && system_setting.GetDiscordSettings().ClientId == "" {
			common.ApiErrorMsg(c, "Discord Client ID must be configured first")
			return
		}
	case "oidc.enabled":
		if value == "true" && system_setting.GetOIDCSettings().ClientId == "" {
			common.ApiErrorMsg(c, "OIDC Client ID must be configured first")
			return
		}
	}

	if err := model.UpdateOption(request.Key, value); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
