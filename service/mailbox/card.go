package mailbox

import (
	"strings"
	"time"
)

const mailboxCardCredentialKind = "mailbox-card:v1"

type mailboxCardSecret struct {
	Number string `json:"card_number"`
	Expiry string `json:"card_expiry"`
}

func normalizeMailboxPAN(raw string) (string, error) {
	pan := strings.NewReplacer(" ", "", "-", "").Replace(raw)
	if len(pan) < 12 || len(pan) > 19 {
		return "", fail(400, "mailbox_import_invalid_card_number")
	}
	sum := 0
	for i := len(pan) - 1; i >= 0; i-- {
		if pan[i] < '0' || pan[i] > '9' {
			return "", fail(400, "mailbox_import_invalid_card_number")
		}
		digit := int(pan[i] - '0')
		if (len(pan)-1-i)%2 == 1 {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	if sum == 0 || sum%10 != 0 {
		return "", fail(400, "mailbox_import_invalid_card_number")
	}
	return pan, nil
}

func normalizeMailboxExpiry(raw string, now time.Time) (string, error) {
	value, err := parseMailboxExpiry(raw)
	if err != nil {
		return "", err
	}
	// Pool operations use China's calendar month even on UTC deployments.
	now = now.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	month := int(value[0]-'0')*10 + int(value[1]-'0')
	year := 2000 + int(value[3]-'0')*10 + int(value[4]-'0')
	if year < now.Year() || (year == now.Year() && month < int(now.Month())) {
		return "", fail(400, "mailbox_import_invalid_card_expiry")
	}
	return value, nil
}

func parseMailboxExpiry(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	invalid := func() (string, error) { return "", fail(400, "mailbox_import_invalid_card_expiry") }
	if len(value) == 7 && value[2] == '/' && strings.HasPrefix(value[3:], "20") {
		value = value[:3] + value[5:]
	}
	if len(value) != 5 || value[2] != '/' {
		return invalid()
	}
	for _, i := range []int{0, 1, 3, 4} {
		if value[i] < '0' || value[i] > '9' {
			return invalid()
		}
	}
	month := int(value[0]-'0')*10 + int(value[1]-'0')
	if month < 1 || month > 12 {
		return invalid()
	}
	return value, nil
}
