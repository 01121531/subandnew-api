package mailbox

import (
	"net/mail"
	"net/url"
	"strings"
)

// Packed rows are accepted only with one unambiguous delimiter. Never trim
// individual fields here: password whitespace is part of the credential.
func unpackMailboxImportCells(cells []string) ([]string, bool) {
	if len(cells) != 1 {
		return cells, false
	}
	value := cells[0]
	hasTabs, hasDashes := strings.Contains(value, "\t"), strings.Contains(value, "----")
	if hasTabs == hasDashes {
		return cells, false
	}
	delimiter := "----"
	if hasTabs {
		delimiter = "\t"
	}
	return strings.Split(value, delimiter), true
}

func normalizeMailboxImportCells(cells []string, kind string) ([]string, []string) {
	cells, packed := unpackMailboxImportCells(cells)
	var notices []string
	if packed {
		notices = append(notices, "mailbox_import_packed_row")
	}
	if kind == AccountTypeRefund && len(cells) == 4 {
		address := strings.TrimSpace(cells[2])
		parsed, err := mail.ParseAddress(address)
		if err == nil && parsed.Name == "" && parsed.Address == address && strings.Contains(address, "@") {
			cells = []string{cells[0], cells[1], cells[3]}
			notices = append(notices, "mailbox_import_recovery_email_ignored")
		}
	}
	if len(cells) >= 3 {
		// Only a literal Base32 secret in the fragment is supported. No HTTP
		// requests, query-token interpretation, or page scraping are permitted.
		link, err := url.Parse(strings.TrimSpace(cells[2]))
		if err == nil && link.Scheme == "https" && link.Hostname() != "" && link.User == nil && link.RawQuery == "" && !link.ForceQuery && link.Fragment != "" && !strings.Contains(link.Fragment, ":") {
			if config, err := parseMailboxOTP(link.Fragment); err == nil {
				cells = append([]string(nil), cells...)
				cells[2] = config.Secret
				notices = append(notices, "mailbox_import_otp_link_extracted")
			}
		}
	}
	return cells, notices
}
