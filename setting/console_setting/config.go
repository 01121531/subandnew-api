package console_setting

import "github.com/01121531/subandnew-api/setting/config"

// ConsoleSetting contains the remaining control-plane header announcement
// feed. Public portal API cards, FAQ, and Uptime Kuma panels were removed.
type ConsoleSetting struct {
	Announcements        string `json:"announcements"`
	AnnouncementsEnabled bool   `json:"announcements_enabled"`
}

var consoleSetting = ConsoleSetting{Announcements: "", AnnouncementsEnabled: true}

func init() {
	config.GlobalConfig.Register("console_setting", &consoleSetting)
}

func GetConsoleSetting() *ConsoleSetting {
	return &consoleSetting
}
