package mailbox

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const mailboxCVVCredentialKind = "mailbox-cvv:v1"

var cvvPattern = regexp.MustCompile(`^[0-9]{3,4}$`)

func temporaryCVVEnabled() bool { return temporaryCVVUnavailableReason() == "" }
func temporaryCVVUnavailableReason() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAILBOX_TEMP_CVV_MODE"))) {
	case "", "database", "single_node", "redis":
		return ""
	default:
		return "not_enabled"
	}
}

func (s *Service) saveCVV(id int64, value string) error {
	if !temporaryCVVEnabled() {
		return fail(503, "mailbox_cvv_disabled")
	}
	if !cvvPattern.MatchString(value) {
		return fail(400, "mailbox_invalid_cvv")
	}
	cipher, err := s.Cipher()
	if err != nil {
		return fail(503, "mailbox_credentials_unavailable")
	}
	encoded, key, _, err := cipher.Encrypt(id, mailboxCVVCredentialKind, managedinstance.CredentialPayload{Secret: value})
	if err != nil {
		return fail(503, "mailbox_credentials_unavailable")
	}
	row := model.MailboxCVV{AccountID: id, Ciphertext: encoded, KeyVersion: key, Version: 1, UpdatedAt: s.Now().Unix()}
	return s.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "account_id"}}, DoUpdates: clause.Assignments(map[string]any{
		"ciphertext": encoded, "key_version": key, "version": gorm.Expr("version + 1"), "updated_at": row.UpdatedAt,
	})}).Create(&row).Error
}

func (s *Service) temporaryCVVMetadata(actor Actor, view *AccountView) {
	if actor.Admin != nil || view.AccountType != AccountTypeOpening {
		return
	}
	view.TemporaryCVVStatus = "unavailable"
	if !temporaryCVVEnabled() {
		view.TemporaryCVVStatus = "disabled"
		return
	}
	if view.Status != StatusPending && view.Status != StatusRejected {
		return
	}
	var row model.MailboxCVV
	if s.DB.Select("account_id", "version").First(&row, "account_id = ?", view.ID).Error == nil {
		view.TemporaryCVVStatus = "available"
		view.TemporaryCVVID = strconv.FormatInt(view.ID, 10) + ":" + strconv.FormatInt(row.Version, 10) + ":" + strconv.FormatInt(view.Version, 10)
	}
}

type TemporaryCVVInput struct {
	Version int64  `json:"version"`
	CVV     string `json:"cvv"`
}
type TemporaryCVVView struct {
	ExpiresAt         int64 `json:"expires_at"`
	StartsOnFirstView bool  `json:"starts_on_first_view"`
	Persistent        bool  `json:"persistent"`
}

func (s *Service) ProvideTemporaryCVV(ctx context.Context, actor Actor, id int64, input TemporaryCVVInput, accountTypes ...string) (*TemporaryCVVView, error) {
	if !temporaryCVVEnabled() {
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	if !cvvPattern.MatchString(input.CVV) {
		return nil, fail(400, "mailbox_invalid_cvv")
	}
	err := s.mutateCVV(ctx, actor, id, input, false, accountTypes...)
	if err != nil {
		return nil, err
	}
	return &TemporaryCVVView{Persistent: true}, nil
}

func (s *Service) ClearCVV(ctx context.Context, actor Actor, id int64, input TemporaryCVVInput, accountTypes ...string) error {
	return s.mutateCVV(ctx, actor, id, input, true, accountTypes...)
}

func (s *Service) mutateCVV(ctx context.Context, actor Actor, id int64, input TemporaryCVVInput, remove bool, accountTypes ...string) (err error) {
	if actor.Admin == nil {
		return fail(403, "mailbox_permission_denied")
	}
	op := "cvv_update"
	if remove {
		op = "cvv_clear"
	}
	defer func() { s.auditMailboxFailure(actor, op, id, err) }()
	s, err = s.withAccountType("", accountTypes...)
	if err != nil {
		return err
	}
	if s.pool() != AccountTypeOpening || input.Version <= 0 {
		return fail(400, "mailbox_invalid_cvv")
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		t := s.WithDB(tx)
		if err := t.workflowActor(actor, authz.MailboxManage); err != nil {
			return err
		}
		if err := t.CheckActor(actor, authz.MailboxCredentials); err != nil {
			return err
		}
		account, err := t.workflowAccount(id)
		if err != nil {
			return err
		}
		if account.Version != input.Version {
			return fail(409, "mailbox_version_conflict")
		}
		if remove {
			err = tx.Delete(&model.MailboxCVV{}, "account_id = ?", id).Error
		} else {
			err = t.saveCVV(id, input.CVV)
		}
		if err != nil {
			return err
		}
		return t.Audit(actor, op, id, account.ActiveAssignmentID, 200, "")
	})
}

func (s *Service) claimTemporaryCVV(actor Actor, account *model.MailboxAccount) (*CredentialView, error) {
	if account.AccountType != AccountTypeOpening {
		return nil, fail(403, "mailbox_permission_denied")
	}
	if !temporaryCVVEnabled() {
		return nil, fail(503, "mailbox_cvv_disabled")
	}
	var assignment model.MailboxAssignment
	if actor.Admin == nil {
		if err := s.DB.Where("id = ? AND account_id = ? AND operator_id = ? AND revoked_at = 0", account.ActiveAssignmentID, account.ID, actor.OperatorID).First(&assignment).Error; err != nil {
			return nil, fail(403, "mailbox_credentials_revoked")
		}
		if assignment.Status == StatusSubmitted {
			return nil, fail(403, "mailbox_cvv_task_restricted")
		}
		if assignment.Status != StatusPending && assignment.Status != StatusRejected {
			return nil, fail(403, "mailbox_credentials_revoked")
		}
	}
	var row model.MailboxCVV
	if err := s.DB.First(&row, "account_id = ?", account.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fail(404, "mailbox_cvv_unavailable")
		}
		return nil, fail(503, "mailbox_credentials_unavailable")
	}
	cipher, err := s.Cipher()
	if err != nil {
		return nil, fail(503, "mailbox_credentials_unavailable")
	}
	payload, err := cipher.Decrypt(account.ID, mailboxCVVCredentialKind, row.KeyVersion, row.Ciphertext)
	if err != nil || !cvvPattern.MatchString(payload.Secret) {
		return nil, fail(503, "mailbox_credentials_unavailable")
	}
	if err := s.Audit(actor, "credentials_cvv", account.ID, account.ActiveAssignmentID, 200, ""); err != nil {
		return nil, fail(500, "mailbox_service_unavailable")
	}
	current, err := s.credentialAccount(actor, account.ID)
	if err != nil {
		return nil, err
	}
	if current.Version != account.Version || current.ActiveAssignmentID != account.ActiveAssignmentID {
		return nil, fail(409, "mailbox_version_conflict")
	}
	if actor.Admin == nil {
		var count int64
		if err := s.DB.Model(&model.MailboxAssignment{}).Where("id = ? AND version = ? AND revoked_at = 0 AND status IN ?", assignment.ID, assignment.Version, []string{StatusPending, StatusRejected}).Count(&count).Error; err != nil || count != 1 {
			return nil, fail(403, "mailbox_credentials_revoked")
		}
	}
	var latest model.MailboxCVV
	if s.DB.First(&latest, "account_id = ?", account.ID).Error != nil || latest.Version != row.Version || latest.Ciphertext != row.Ciphertext {
		return nil, fail(409, "mailbox_version_conflict")
	}
	return &CredentialView{Available: true, CVV: payload.Secret, Persistent: true, ServerTime: s.Now().Unix()}, nil
}
