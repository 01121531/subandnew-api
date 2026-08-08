package model

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/setting/config"
	"github.com/01121531/HUICHUAN-AI/setting/performance_setting"
	"github.com/01121531/HUICHUAN-AI/setting/system_setting"
	"gorm.io/gorm"
)

type Option struct {
	Key   string `json:"key" gorm:"primaryKey"`
	Value string `json:"value"`
}

var controlPlaneOptionKeys = map[string]struct{}{
	"PasswordLoginEnabled": {}, "EmailVerificationEnabled": {},
	"GitHubOAuthEnabled": {}, "LinuxDOOAuthEnabled": {}, "TelegramOAuthEnabled": {}, "WeChatAuthEnabled": {},
	"TurnstileCheckEnabled": {}, "EmailDomainRestrictionEnabled": {}, "EmailAliasRestrictionEnabled": {},
	"EmailDomainWhitelist": {}, "SMTPServer": {}, "SMTPFrom": {}, "SMTPPort": {}, "SMTPAccount": {}, "SMTPToken": {},
	"SMTPSSLEnabled": {}, "SMTPStartTLSEnabled": {}, "SMTPInsecureSkipVerify": {}, "SMTPForceAuthLogin": {},
	"Notice": {}, "About": {}, "Footer": {}, "SystemName": {}, "Logo": {}, "ServerAddress": {},
	"GitHubClientId": {}, "GitHubClientSecret": {}, "LinuxDOClientId": {}, "LinuxDOClientSecret": {}, "LinuxDOMinimumTrustLevel": {},
	"TelegramBotToken": {}, "TelegramBotName": {}, "WeChatServerAddress": {}, "WeChatServerToken": {},
	"WeChatAccountQRCodeImageURL": {}, "TurnstileSiteKey": {}, "TurnstileSecretKey": {},
}

var controlPlaneConfigPrefixes = []string{
	"console_setting.", "discord.", "legal.", "oidc.", "passkey.", "performance_setting.",
}

func IsControlPlaneOptionKey(key string) bool {
	if _, ok := controlPlaneOptionKeys[key]; ok {
		return true
	}
	for _, prefix := range controlPlaneConfigPrefixes {
		if strings.HasPrefix(key, prefix) {
			return config.GlobalConfig.Get(strings.TrimSuffix(prefix, ".")) != nil
		}
	}
	return false
}

func AllOption() ([]*Option, error) {
	var stored []*Option
	if err := DB.Find(&stored).Error; err != nil {
		return nil, err
	}
	options := make([]*Option, 0, len(stored))
	for _, option := range stored {
		if IsControlPlaneOptionKey(option.Key) {
			options = append(options, option)
		}
	}
	return options, nil
}

func controlPlaneOptionDefaults() map[string]string {
	defaults := map[string]string{
		"PasswordLoginEnabled":          strconv.FormatBool(common.PasswordLoginEnabled),
		"EmailVerificationEnabled":      strconv.FormatBool(common.EmailVerificationEnabled),
		"GitHubOAuthEnabled":            strconv.FormatBool(common.GitHubOAuthEnabled),
		"LinuxDOOAuthEnabled":           strconv.FormatBool(common.LinuxDOOAuthEnabled),
		"TelegramOAuthEnabled":          strconv.FormatBool(common.TelegramOAuthEnabled),
		"WeChatAuthEnabled":             strconv.FormatBool(common.WeChatAuthEnabled),
		"TurnstileCheckEnabled":         strconv.FormatBool(common.TurnstileCheckEnabled),
		"EmailDomainRestrictionEnabled": strconv.FormatBool(common.EmailDomainRestrictionEnabled),
		"EmailAliasRestrictionEnabled":  strconv.FormatBool(common.EmailAliasRestrictionEnabled),
		"EmailDomainWhitelist":          strings.Join(common.EmailDomainWhitelist, ","),
		"SMTPServer":                    common.SMTPServer, "SMTPFrom": common.SMTPFrom, "SMTPPort": strconv.Itoa(common.SMTPPort),
		"SMTPAccount": common.SMTPAccount, "SMTPToken": common.SMTPToken,
		"SMTPSSLEnabled":         strconv.FormatBool(common.SMTPSSLEnabled),
		"SMTPStartTLSEnabled":    strconv.FormatBool(common.SMTPStartTLSEnabled),
		"SMTPInsecureSkipVerify": strconv.FormatBool(common.SMTPInsecureSkipVerify),
		"SMTPForceAuthLogin":     strconv.FormatBool(common.SMTPForceAuthLogin),
		"Notice":                 "", "About": "", "Footer": common.Footer, "SystemName": common.SystemName, "Logo": common.Logo,
		"ServerAddress":  system_setting.ServerAddress,
		"GitHubClientId": common.GitHubClientId, "GitHubClientSecret": common.GitHubClientSecret,
		"LinuxDOClientId": common.LinuxDOClientId, "LinuxDOClientSecret": common.LinuxDOClientSecret,
		"LinuxDOMinimumTrustLevel": strconv.Itoa(common.LinuxDOMinimumTrustLevel),
		"TelegramBotToken":         common.TelegramBotToken, "TelegramBotName": common.TelegramBotName,
		"WeChatServerAddress": common.WeChatServerAddress, "WeChatServerToken": common.WeChatServerToken,
		"WeChatAccountQRCodeImageURL": common.WeChatAccountQRCodeImageURL,
		"TurnstileSiteKey":            common.TurnstileSiteKey, "TurnstileSecretKey": common.TurnstileSecretKey,
	}
	for key, value := range config.GlobalConfig.ExportAllConfigs() {
		if IsControlPlaneOptionKey(key) {
			defaults[key] = value
		}
	}
	return defaults
}

func InitOptionMap() {
	common.OptionMapRWMutex.Lock()
	common.OptionMap = controlPlaneOptionDefaults()
	common.OptionMapRWMutex.Unlock()
	loadOptionsFromDatabase()
}

func loadOptionsFromDatabase() {
	options, err := AllOption()
	if err != nil {
		common.SysLog("failed to load options: " + err.Error())
		return
	}
	for _, option := range options {
		if err := updateOptionMap(option.Key, option.Value); err != nil {
			common.SysLog("failed to apply option " + option.Key + ": " + err.Error())
		}
	}
}

func SyncOptions(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		loadOptionsFromDatabase()
	}
}

func UpdateOption(key, value string) error {
	if !IsControlPlaneOptionKey(key) {
		return errors.New("unsupported control-plane option")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		option := Option{Key: key}
		if err := tx.FirstOrCreate(&option, Option{Key: key}).Error; err != nil {
			return err
		}
		option.Value = value
		if err := tx.Save(&option).Error; err != nil {
			return err
		}
		return updateOptionMap(key, value)
	})
}

func updateOptionMap(key, value string) error {
	if !IsControlPlaneOptionKey(key) {
		return errors.New("unsupported control-plane option")
	}
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	common.OptionMap[key] = value

	if strings.Contains(key, ".") {
		parts := strings.SplitN(key, ".", 2)
		cfg := config.GlobalConfig.Get(parts[0])
		if cfg == nil {
			return errors.New("unknown control-plane configuration")
		}
		if err := config.UpdateConfigFromMap(cfg, map[string]string{parts[1]: value}); err != nil {
			return err
		}
		if parts[0] == "performance_setting" {
			performance_setting.UpdateAndSync()
		}
		return nil
	}

	boolValue := value == "true"
	switch key {
	case "PasswordLoginEnabled":
		common.PasswordLoginEnabled = boolValue
	case "EmailVerificationEnabled":
		common.EmailVerificationEnabled = boolValue
	case "GitHubOAuthEnabled":
		common.GitHubOAuthEnabled = boolValue
	case "LinuxDOOAuthEnabled":
		common.LinuxDOOAuthEnabled = boolValue
	case "TelegramOAuthEnabled":
		common.TelegramOAuthEnabled = boolValue
	case "WeChatAuthEnabled":
		common.WeChatAuthEnabled = boolValue
	case "TurnstileCheckEnabled":
		common.TurnstileCheckEnabled = boolValue
	case "EmailDomainRestrictionEnabled":
		common.EmailDomainRestrictionEnabled = boolValue
	case "EmailAliasRestrictionEnabled":
		common.EmailAliasRestrictionEnabled = boolValue
	case "SMTPSSLEnabled":
		common.SMTPSSLEnabled = boolValue
	case "SMTPStartTLSEnabled":
		common.SMTPStartTLSEnabled = boolValue
	case "SMTPInsecureSkipVerify":
		common.SMTPInsecureSkipVerify = boolValue
	case "SMTPForceAuthLogin":
		common.SMTPForceAuthLogin = boolValue
	case "EmailDomainWhitelist":
		common.EmailDomainWhitelist = strings.Split(value, ",")
	case "SMTPServer":
		common.SMTPServer = value
	case "SMTPFrom":
		common.SMTPFrom = value
	case "SMTPAccount":
		common.SMTPAccount = value
	case "SMTPToken":
		common.SMTPToken = value
	case "SMTPPort":
		common.SMTPPort, _ = strconv.Atoi(value)
	case "Footer":
		common.Footer = value
	case "SystemName":
		common.SystemName = value
	case "Logo":
		common.Logo = value
	case "ServerAddress":
		system_setting.ServerAddress = value
	case "GitHubClientId":
		common.GitHubClientId = value
	case "GitHubClientSecret":
		common.GitHubClientSecret = value
	case "LinuxDOClientId":
		common.LinuxDOClientId = value
	case "LinuxDOClientSecret":
		common.LinuxDOClientSecret = value
	case "LinuxDOMinimumTrustLevel":
		common.LinuxDOMinimumTrustLevel, _ = strconv.Atoi(value)
	case "TelegramBotToken":
		common.TelegramBotToken = value
	case "TelegramBotName":
		common.TelegramBotName = value
	case "WeChatServerAddress":
		common.WeChatServerAddress = value
	case "WeChatServerToken":
		common.WeChatServerToken = value
	case "WeChatAccountQRCodeImageURL":
		common.WeChatAccountQRCodeImageURL = value
	case "TurnstileSiteKey":
		common.TurnstileSiteKey = value
	case "TurnstileSecretKey":
		common.TurnstileSecretKey = value
	}
	return nil
}
