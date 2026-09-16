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
	delimiters := []string{"\t", "----", "|"}
	var present []string
	for _, delimiter := range delimiters {
		if strings.Contains(value, delimiter) {
			present = append(present, delimiter)
		}
	}
	if len(present) == 1 {
		return strings.Split(value, present[0]), true
	}
	// A different delimiter inside extra metadata must not obscure an explicit
	// credential prefix. Mixed delimiters in the credentials are rejected.
	for _, delimiter := range present {
		parts := strings.Split(value, delimiter)
		if len(parts) < 3 || !mailboxImportEmail(parts[0]) {
			continue
		}
		prefix := 3
		if mailboxImportEmail(parts[2]) || strings.TrimSpace(parts[2]) == "" {
			prefix = 4
		}
		if len(parts) <= prefix {
			continue
		}
		mixed := false
		for _, field := range parts[:prefix] {
			for _, other := range present {
				mixed = mixed || (other != delimiter && strings.Contains(field, other))
			}
		}
		if !mixed {
			return parts, true
		}
	}
	return cells, false
}

func (s *Service) WithImportExtraFields(ignore bool) *Service {
	copy := *s
	copy.importIgnoreExtraFields = ignore
	return &copy
}

func (s *Service) WithPartialImport(allow bool) *Service {
	copy := *s
	copy.importAllowPartial = allow
	return &copy
}

func mailboxImportEmail(value string) bool {
	address := strings.TrimSpace(value)
	parsed, err := mail.ParseAddress(address)
	return err == nil && parsed.Name == "" && parsed.Address == address && strings.Contains(address, "@")
}

func normalizeMailboxImportCells(cells []string, kind string, ignoreExtra bool) ([]string, []string) {
	cells, packed := unpackMailboxImportCells(cells)
	var notices []string
	if packed {
		notices = append(notices, "mailbox_import_packed_row")
	}
	if kind == AccountTypeRefund {
		original := len(cells)
		for len(cells) > 3 && strings.TrimSpace(cells[len(cells)-1]) == "" {
			cells = cells[:len(cells)-1]
		}
		if original != len(cells) {
			notices = append(notices, "mailbox_import_empty_fields_ignored")
		}
		if len(cells) >= 4 && (mailboxImportEmail(cells[2]) || strings.TrimSpace(cells[2]) == "") {
			if strings.TrimSpace(cells[2]) == "" {
				if original == len(cells) {
					notices = append(notices, "mailbox_import_empty_fields_ignored")
				}
			} else {
				notices = append(notices, "mailbox_import_recovery_email_ignored")
			}
			cells = append([]string{cells[0], cells[1]}, cells[3:]...)
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
		groups := strings.Fields(cells[2])
		grouped := len(groups) > 1
		for _, group := range groups {
			grouped = grouped && len(group) == 4
		}
		if grouped && strings.Contains(cells[2], " ") && !strings.Contains(cells[2], ":") {
			if config, err := parseMailboxOTP(strings.ReplaceAll(cells[2], " ", "")); err == nil {
				cells = append([]string(nil), cells...)
				cells[2] = config.Secret
				notices = append(notices, "mailbox_import_otp_spaces_removed")
			}
		}
	}
	if kind == AccountTypeRefund && ignoreExtra && len(cells) > 3 && mailboxImportEmail(cells[0]) {
		if _, err := parseMailboxOTP(cells[2]); err == nil {
			cells = cells[:3]
			notices = append(notices, "mailbox_import_extra_fields_ignored")
		}
	}
	return cells, notices
}
