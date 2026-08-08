package console_setting

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

var announcementTypes = map[string]bool{
	"default": true, "ongoing": true, "success": true,
	"warning": true, "error": true,
}

func ValidateConsoleSettings(value string, settingType string) error {
	if settingType != "Announcements" {
		return fmt.Errorf("unsupported console setting %q", settingType)
	}
	if value == "" {
		return nil
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		return fmt.Errorf("announcements must be a JSON array: %w", err)
	}
	if len(items) > 100 {
		return fmt.Errorf("announcements cannot contain more than 100 items")
	}
	for index, item := range items {
		content, ok := item["content"].(string)
		if !ok || strings.TrimSpace(content) == "" || len(content) > 500 {
			return fmt.Errorf("announcement %d has invalid content", index+1)
		}
		publishedAt, ok := item["publishDate"].(string)
		if !ok {
			return fmt.Errorf("announcement %d has no publishDate", index+1)
		}
		if _, err := time.Parse(time.RFC3339, publishedAt); err != nil {
			return fmt.Errorf("announcement %d has invalid publishDate", index+1)
		}
		if kind, exists := item["type"].(string); exists && !announcementTypes[kind] {
			return fmt.Errorf("announcement %d has invalid type", index+1)
		}
		if extra, exists := item["extra"].(string); exists && len(extra) > 200 {
			return fmt.Errorf("announcement %d extra text is too long", index+1)
		}
	}
	return nil
}

func GetAnnouncements() []map[string]any {
	var items []map[string]any
	if err := json.Unmarshal([]byte(GetConsoleSetting().Announcements), &items); err != nil {
		return []map[string]any{}
	}
	sort.SliceStable(items, func(left int, right int) bool {
		return announcementTime(items[left]).After(announcementTime(items[right]))
	})
	return items
}

func announcementTime(item map[string]any) time.Time {
	value, _ := item["publishDate"].(string)
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}
