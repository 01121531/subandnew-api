package builtin

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const assistantBusinessTextLimit = 300

var assistantSecretTextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:https?|wss?)://[^\s]+`),
	regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?\b`),
	regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\bsk-[a-z0-9_-]{8,}`),
	regexp.MustCompile(`(?i)\b(?:api[_ -]?key|access[_ -]?token|refresh[_ -]?token|token|password|passwd|secret|cookie)\s*[:=]\s*[^\s,;]+`),
}

func safeBusinessText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, pattern := range assistantSecretTextPatterns {
		value = pattern.ReplaceAllString(value, "[已隐藏]")
	}
	if utf8.RuneCountInString(value) <= assistantBusinessTextLimit {
		return value
	}
	runes := []rune(value)
	return string(runes[:assistantBusinessTextLimit]) + "..."
}
