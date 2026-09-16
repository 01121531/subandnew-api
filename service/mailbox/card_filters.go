package mailbox

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CardFilter struct {
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

var cardFilterDigits = regexp.MustCompile(`^[0-9]{1,19}$`)

func (s *Service) cardChanged(account model.MailboxAccount, number string) error {
	previous := ""
	if account.CardCiphertext != "" {
		cipher, err := s.Cipher()
		if err != nil {
			return fail(503, "mailbox_credentials_unavailable")
		}
		payload, err := cipher.Decrypt(account.ID, mailboxCardCredentialKind, account.CardKeyVersion, account.CardCiphertext)
		var secret mailboxCardSecret
		if err != nil || json.Unmarshal([]byte(payload.Secret), &secret) != nil {
			return fail(503, "mailbox_credentials_unavailable")
		}
		previous = secret.Number
	}
	if previous != number {
		return s.DB.Delete(&model.MailboxCVV{}, "account_id = ?", account.ID).Error
	}
	return nil
}

// Called in the same transaction as the card mutation.
func (s *Service) indexCard(id int64, number, ciphertext string) error {
	cipher, err := s.Cipher()
	if err != nil {
		return fail(503, "mailbox_credentials_unavailable")
	}
	if err := s.DB.Delete(&model.MailboxCardIndex{}, "account_id = ?", id).Error; err != nil {
		return err
	}
	rows := []model.MailboxCardIndex{}
	for length := 1; length <= len(number); length++ {
		for kind, value := range map[string]string{"prefix": number[:length], "suffix": number[len(number)-length:]} {
			rows = append(rows, model.MailboxCardIndex{AccountID: id, Kind: kind, Length: length, Digest: cipher.MailboxCardDigest(kind, value)})
		}
	}
	if len(rows) > 0 {
		if err := s.DB.Create(&rows).Error; err != nil {
			return err
		}
	}
	state := model.MailboxCardIndexState{AccountID: id, Fingerprint: cipher.MailboxCardDigest("ready", "v1")}
	return s.DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&state).Error
}

// Bounded, resumable backfill. A partial/failed index never yields partial results.
func (s *Service) ensureCardIndex() error {
	cipher, err := s.Cipher()
	if err != nil {
		return fail(503, "mailbox_credentials_unavailable")
	}
	pending := func() *gorm.DB {
		return s.DB.Model(&model.MailboxAccount{}).Where("account_type = ? AND card_ciphertext IS NOT NULL AND card_ciphertext <> ''", AccountTypeOpening).
			Where("id NOT IN (?)", s.DB.Model(&model.MailboxCardIndexState{}).Select("account_id").Where("fingerprint = ?", cipher.MailboxCardDigest("ready", "v1")))
	}
	var accounts []model.MailboxAccount
	if err := pending().Order("id").Limit(250).Find(&accounts).Error; err != nil {
		return fail(503, "mailbox_card_index_unavailable")
	}
	for _, account := range accounts {
		err := s.DB.Transaction(func(tx *gorm.DB) error {
			var current model.MailboxAccount
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, account.ID).Error; err != nil {
				return err
			}
			// CAS serializes SQLite against simultaneous imports/card correction.
			if err := workflowCAS(tx.Model(&model.MailboxAccount{}).Where("id = ? AND version = ?", current.ID, current.Version).Update("version", gorm.Expr("version + 1"))); err != nil {
				return err
			}
			payload, err := cipher.Decrypt(current.ID, mailboxCardCredentialKind, current.CardKeyVersion, current.CardCiphertext)
			var secret mailboxCardSecret
			if err != nil || json.Unmarshal([]byte(payload.Secret), &secret) != nil {
				return fail(503, "mailbox_card_index_unavailable")
			}
			number, err := normalizeMailboxPAN(secret.Number)
			if err != nil {
				return fail(503, "mailbox_card_index_unavailable")
			}
			return s.WithDB(tx).indexCard(current.ID, number, current.CardCiphertext)
		})
		if err != nil {
			return fail(503, "mailbox_card_index_unavailable")
		}
	}
	var remaining int64
	if pending().Count(&remaining).Error != nil || remaining > 0 {
		return fail(503, "mailbox_card_index_unavailable")
	}
	return nil
}

func (s *Service) filterCards(actor Actor, q *gorm.DB, filters []CardFilter) (*gorm.DB, error) {
	if len(filters) == 0 {
		return q, nil
	}
	if actor.Admin == nil {
		return nil, fail(403, "mailbox_permission_denied")
	}
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return nil, err
	}
	if err := s.CheckActor(actor, authz.MailboxCredentials); err != nil {
		return nil, err
	}
	if s.pool() != AccountTypeOpening || len(filters) > 4 {
		return nil, fail(400, "mailbox_invalid_card_filter")
	}
	normalized := make([]CardFilter, 0, len(filters))
	for _, filter := range filters {
		switch filter.Operator {
		case "starts_with", "not_starts_with", "ends_with", "not_ends_with":
		default:
			return nil, fail(400, "mailbox_invalid_card_filter")
		}
		seen := map[string]bool{}
		values := []string{}
		for _, entry := range filter.Values {
			for _, value := range strings.FieldsFunc(entry, func(r rune) bool { return r == ',' || r == '，' || r == '\n' || r == '\r' }) {
				value = strings.TrimSpace(value)
				if !cardFilterDigits.MatchString(value) {
					return nil, fail(400, "mailbox_invalid_card_filter")
				}
				if !seen[value] {
					seen[value] = true
					values = append(values, value)
				}
			}
		}
		if len(values) == 0 || len(values) > 1000 {
			return nil, fail(400, "mailbox_invalid_card_filter")
		}
		normalized = append(normalized, CardFilter{Operator: filter.Operator, Values: values})
	}
	if err := s.ensureCardIndex(); err != nil {
		return nil, err
	}
	cipher, err := s.Cipher()
	if err != nil {
		return nil, fail(503, "mailbox_credentials_unavailable")
	}
	for _, filter := range normalized {
		kind := "prefix"
		if strings.HasSuffix(filter.Operator, "ends_with") {
			kind = "suffix"
		}
		digests := make([]string, 0, len(filter.Values))
		for _, value := range filter.Values {
			digests = append(digests, cipher.MailboxCardDigest(kind, value))
		}
		ids := s.DB.Model(&model.MailboxCardIndex{}).Select("account_id").Where("kind = ? AND digest IN ?", kind, digests)
		if strings.HasPrefix(filter.Operator, "not_") {
			q = q.Where("a.id NOT IN (?)", ids)
		} else {
			q = q.Where("a.id IN (?)", ids)
		}
	}
	return q, nil
}
