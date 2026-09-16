package mailbox

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

const poolTestPAN = "4242424242424242"

func poolTestCSV(email string) []byte {
	var out bytes.Buffer
	w := csv.NewWriter(&out)
	_ = w.Write([]string{email, "synthetic-password", mailboxImportTestSecret, poolTestPAN, "12/99"})
	w.Flush()
	return out.Bytes()
}

func poolTestOpening(t *testing.T, s *Service, admin Actor, email string) model.MailboxAccount {
	t.Helper()
	_, err := s.Import(context.Background(), admin, "csv", poolTestCSV(email), AccountTypeOpening)
	require.NoError(t, err)
	var account model.MailboxAccount
	require.NoError(t, s.DB.Where("account_type = ? AND email = ?", AccountTypeOpening, email).First(&account).Error)
	return account
}

func TestMailboxOpeningImportFormatsAndCipherIsolation(t *testing.T) {
	for _, format := range []string{"text", "csv", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			s, admin := newMailboxImportTestService(t)
			refund := mailboxImportTestAccount(t, s, admin, "same@example.test", "refund-password", mailboxImportTestSecret)
			cells := []string{"same@example.test", "  synthetic,\"password\"\r\n  ", mailboxImportTestSecret, "4242-4242 4242-4242", "12/99"}
			header := []string{"email", "password", "2fa", "card_number", "card_expiry"}
			var data []byte
			switch format {
			case "text":
				cells[1] = "  synthetic-password  "
				data = []byte(strings.Join(cells, "----"))
			case "csv":
				var out bytes.Buffer
				w := csv.NewWriter(&out)
				require.NoError(t, w.WriteAll([][]string{header, cells}))
				data = out.Bytes()
			case "xlsx":
				data = mailboxImportTestWorkbook(t, [][]string{header, cells}, "")
			}
			preview, err := s.PreviewImport(context.Background(), admin, format, data, AccountTypeOpening)
			require.NoError(t, err)
			require.True(t, preview.Valid, "%+v", preview.Issues)
			require.Equal(t, AccountTypeOpening, preview.Rows[0].AccountType)
			require.Equal(t, "4242", preview.Rows[0].CardLast4)
			encoded, err := json.Marshal(preview)
			require.NoError(t, err)
			for _, secret := range []string{poolTestPAN, "12/99", "synthetic", mailboxImportTestSecret} {
				require.NotContains(t, string(encoded), secret)
			}
			_, err = s.Import(context.Background(), admin, format, data, AccountTypeOpening)
			require.NoError(t, err)
			var account model.MailboxAccount
			require.NoError(t, s.DB.Where("account_type = ?", AccountTypeOpening).First(&account).Error)
			require.NotEqual(t, refund.ID, account.ID)
			require.NotEmpty(t, account.CardCiphertext)
			require.Equal(t, "mailbox-test-v1", account.CardKeyVersion)
			require.NotEqual(t, account.Ciphertext, account.CardCiphertext)
			cipher, err := s.Cipher()
			require.NoError(t, err)
			payload, err := cipher.Decrypt(account.ID, mailboxCredentialKind, account.KeyVersion, account.Ciphertext)
			require.NoError(t, err)
			require.NotContains(t, payload.Secret, poolTestPAN)
			for _, binding := range []struct {
				id      int64
				kind    string
				version string
			}{{refund.ID, mailboxCardCredentialKind, account.CardKeyVersion}, {account.ID, mailboxCredentialKind, account.CardKeyVersion}, {account.ID, mailboxCardCredentialKind, "wrong-key-version"}} {
				_, err := cipher.Decrypt(binding.id, binding.kind, binding.version, account.CardCiphertext)
				require.Error(t, err)
			}
			password, err := s.Credentials(context.Background(), admin, account.ID, "password", AccountTypeOpening)
			require.NoError(t, err)
			require.Equal(t, cells[1], password.Password)
			card, err := s.Credentials(context.Background(), admin, account.ID, "card", AccountTypeOpening)
			require.NoError(t, err)
			require.Equal(t, poolTestPAN, card.CardNumber)
			require.Equal(t, "12/99", card.CardExpiry)
			require.Empty(t, card.Password)
			require.Empty(t, card.Code)
			encoded, err = json.Marshal(card)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"card_number"`)
			require.Contains(t, string(encoded), `"card_expiry"`)
			_, err = s.Credentials(context.Background(), admin, refund.ID, "card")
			workflowTestStatus(t, err, 400)
			_, err = s.Credentials(context.Background(), admin, account.ID, "card")
			workflowTestStatus(t, err, 404)
			_, err = s.Import(context.Background(), admin, format, data, AccountTypeOpening)
			require.Error(t, err)
			var audits []model.MailboxAudit
			require.NoError(t, s.DB.Where("account_type = ?", AccountTypeOpening).Find(&audits).Error)
			require.NotEmpty(t, audits)
			encoded, err = json.Marshal(audits)
			require.NoError(t, err)
			for _, secret := range []string{poolTestPAN, "12/99", "synthetic-password", mailboxImportTestSecret} {
				require.NotContains(t, string(encoded), secret)
			}
		})
	}
}

func TestMailboxCardValidation(t *testing.T) {
	now := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	for _, pan := range []string{poolTestPAN, "4242-4242 4242-4242", "378282246310005"} {
		_, err := normalizeMailboxPAN(pan)
		require.NoError(t, err)
	}
	for _, pan := range []string{"", "4242424242424241", "0000000000000000", "42424242424", "42424242424242424242", "4242\t424242424242", "4.242424242424242e15", "+4242424242424242", "424242424242424x"} {
		_, err := normalizeMailboxPAN(pan)
		require.Error(t, err, pan)
	}
	for _, expiry := range []string{"09/26", "12/99", " 10/26 ", "09/2026", "12/2099"} {
		_, err := normalizeMailboxExpiry(expiry, now)
		require.NoError(t, err)
	}
	for _, expiry := range []string{"08/26", "12/25", "00/99", "13/99", "9/26", "09/3026", "09/026", "09-26", "aa/bb", ""} {
		_, err := normalizeMailboxExpiry(expiry, now)
		require.Error(t, err, expiry)
	}
}

func TestMailboxOpeningImportRejectsColumnsNumericPANAndIsAtomic(t *testing.T) {
	s, admin := newMailboxImportTestService(t)
	ctx := context.Background()
	valid := string(poolTestCSV("valid@example.test"))
	for _, data := range []string{
		valid + "bad@example.test,synthetic," + mailboxImportTestSecret + "\n",
		valid + strings.ReplaceAll(string(poolTestCSV("bad@example.test")), poolTestPAN, "4242424242424241"),
		strings.TrimSpace(valid) + ",123\n",
		"email,password,2fa,card_number,cvv\n" + valid,
		"email,password,2fa,card_number,card_expiry,cvv\n" + valid,
	} {
		_, err := s.Import(ctx, admin, "csv", []byte(data), AccountTypeOpening)
		require.Error(t, err)
		var count int64
		require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Count(&count).Error)
		require.Zero(t, count)
	}
	_, err := s.Import(ctx, admin, "csv", []byte(valid))
	require.Error(t, err)
	book := excelize.NewFile()
	defer book.Close()
	row := []interface{}{"numeric@example.test", "synthetic", mailboxImportTestSecret, int64(4242424242424242), "12/99"}
	require.NoError(t, book.SetSheetRow("Sheet1", "A1", &row))
	data, err := book.WriteToBuffer()
	require.NoError(t, err)
	_, err = s.Import(ctx, admin, "xlsx", data.Bytes(), AccountTypeOpening)
	require.EqualError(t, err, "mailbox_import_invalid")
	preview, err := s.PreviewImport(ctx, admin, "xlsx", data.Bytes(), AccountTypeOpening)
	require.NoError(t, err)
	require.False(t, preview.Valid)
	require.Equal(t, 1, preview.Total)
	require.Equal(t, []ImportIssue{{Row: 1, Code: "mailbox_import_card_number_text_required"}}, preview.Issues)
	encoded, err := json.Marshal(preview)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), poolTestPAN)
	var batch bytes.Buffer
	for i := 0; i < 1001; i++ {
		batch.Write(poolTestCSV(fmt.Sprintf("row%d@example.test", i)))
	}
	_, err = s.Import(ctx, admin, "csv", batch.Bytes(), AccountTypeOpening)
	require.EqualError(t, err, "mailbox_import_too_many_rows")
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Count(&count).Error)
	require.Zero(t, count)
	// Failure after rows are inserted must roll back both credential blobs.
	require.NoError(t, s.DB.Callback().Create().Before("gorm:create").Register("opening-import-audit-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "mailbox_audits" {
			tx.AddError(errors.New("synthetic audit failure"))
		}
	}))
	_, err = s.Import(ctx, admin, "csv", []byte(valid), AccountTypeOpening)
	require.Error(t, err)
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestMailboxOpeningCredentialsFreshAuthorizationAndCipherChanges(t *testing.T) {
	for _, change := range []string{"approved", "recall", "disabled", "session", "key", "cipher", "last4", "audit"} {
		t.Run(change, func(t *testing.T) {
			s, admin := newMailboxImportTestService(t)
			account := poolTestOpening(t, s, admin, "fresh@example.test")
			actor := mailboxCredentialTestOperator(t, s, account.ID)
			view, err := s.Credentials(context.Background(), actor, account.ID, "card", AccountTypeOpening)
			require.NoError(t, err)
			require.Equal(t, poolTestPAN, view.CardNumber)
			original := s.Cipher
			s.Cipher = func() (*managedinstance.CredentialCipher, error) {
				var err error
				switch change {
				case "approved":
					err = s.DB.Model(&model.MailboxAssignment{}).Where("account_id = ?", account.ID).Update("status", StatusApproved).Error
				case "recall":
					err = s.DB.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("active_assignment_id", 0).Error
				case "disabled":
					err = s.DB.Model(&model.MailboxOperator{}).Where("id = ?", actor.OperatorID).Update("enabled", false).Error
				case "session":
					err = s.DB.Where("token_hash = ?", actor.SessionHash).Delete(&model.MailboxSession{}).Error
				case "key":
					err = s.DB.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("card_key_version", "changed").Error
				case "cipher":
					err = s.DB.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("card_ciphertext", account.Ciphertext).Error
				case "last4":
					err = s.DB.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("card_last4", "0000").Error
				case "audit":
					err = s.DB.Migrator().DropTable(&model.MailboxAudit{})
				}
				require.NoError(t, err)
				return original()
			}
			view, err = s.Credentials(context.Background(), actor, account.ID, "card", AccountTypeOpening)
			require.Error(t, err)
			require.Nil(t, view)
		})
	}
}

func TestMailboxOpeningHistoricalExpiryAndChinaMonth(t *testing.T) {
	before := time.Date(2026, 9, 30, 15, 59, 59, 0, time.UTC)
	_, err := normalizeMailboxExpiry("09/26", before)
	require.NoError(t, err)
	_, err = normalizeMailboxExpiry("09/26", before.Add(time.Second))
	require.Error(t, err)
	s, admin := newMailboxImportTestService(t)
	s.Now = func() time.Time { return before }
	_, err = s.Import(context.Background(), admin, "csv", bytes.ReplaceAll(poolTestCSV("historical@example.test"), []byte("12/99"), []byte("09/26")), AccountTypeOpening)
	require.NoError(t, err)
	var account model.MailboxAccount
	require.NoError(t, s.DB.First(&account).Error)
	s.Now = func() time.Time { return before.AddDate(1, 0, 0) }
	view, err := s.Credentials(context.Background(), admin, account.ID, "card", AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, poolTestPAN, view.CardNumber)
	require.Equal(t, "09/26", view.CardExpiry)
}

func TestMailboxOpeningPermissionsAndAuditType(t *testing.T) {
	s, admin := newMailboxImportTestService(t)
	account := poolTestOpening(t, s, admin, "permissions@example.test")
	require.NoError(t, s.DB.Create(&model.User{Id: 2, Username: "restricted-pool", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	access, err := authz.LoadDataAccess(s.DB, 2)
	require.NoError(t, err)
	view, err := s.Credentials(context.Background(), Actor{Admin: access}, account.ID, "card", AccountTypeOpening)
	require.Error(t, err)
	require.Nil(t, view)
	require.NoError(t, s.Audit(admin, "controller_import", 0, 0, 200, "", AccountTypeOpening))
	require.NoError(t, s.Audit(admin, "operator_event", 0, 0, 200, ""))
	_, err = s.Import(context.Background(), admin, "csv", []byte("invalid"), AccountTypeOpening)
	require.Error(t, err)
	for _, test := range []struct{ action, kind string }{{"controller_import", AccountTypeOpening}, {"operator_event", AccountTypeRefund}, {"import", AccountTypeOpening}} {
		var audit model.MailboxAudit
		require.NoError(t, s.DB.Where("action = ?", test.action).Order("id DESC").First(&audit).Error)
		require.Equal(t, test.kind, audit.AccountType)
	}
	original := s.Cipher
	s.Cipher = func() (*managedinstance.CredentialCipher, error) {
		require.NoError(t, s.DB.Model(&model.User{}).Where("id = ?", admin.Admin.UserID).Update("authorization_version", admin.Admin.Version+1).Error)
		return original()
	}
	view, err = s.Credentials(context.Background(), admin, account.ID, "card", AccountTypeOpening)
	require.Nil(t, view)
	require.ErrorIs(t, err, authz.ErrAuthorizationChanged)
}

func TestMailboxPoolWorkflowIsolationAndRecall(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	cipher, err := managedinstance.NewCredentialCipher(bytes.Repeat([]byte{0x5a}, 32), "synthetic-pool-key")
	require.NoError(t, err)
	s.Cipher = func() (*managedinstance.CredentialCipher, error) { return cipher, nil }
	ctx := context.Background()
	refund := workflowTestAccount(t, s, "same@example.test")
	opening := poolTestOpening(t, s, admin, refund.Email)
	for _, kind := range []string{"", AccountTypeRefund, AccountTypeOpening} {
		page, err := s.ListAccounts(ctx, admin, ListQuery{AccountType: kind})
		require.NoError(t, err)
		require.EqualValues(t, 1, page.Total)
		if kind == AccountTypeOpening {
			require.Equal(t, opening.ID, page.Items[0].ID)
		} else {
			require.Equal(t, refund.ID, page.Items[0].ID)
		}
	}
	_, err = s.ListAccounts(ctx, admin, ListQuery{AccountType: "invalid"})
	workflowTestStatus(t, err, 400)
	_, err = s.ListAccounts(ctx, admin, ListQuery{AccountType: AccountTypeOpening}, AccountTypeRefund)
	workflowTestStatus(t, err, 400)
	_, err = s.GetAccount(ctx, admin, opening.ID)
	workflowTestStatus(t, err, 404)
	err = s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{ID: refund.ID, Version: 1}, {ID: opening.ID, Version: 1}}, OperatorID: owner.OperatorID})
	require.Error(t, err)
	var stored model.MailboxAccount
	require.NoError(t, s.DB.First(&stored, refund.ID).Error)
	require.EqualValues(t, 1, stored.Version)
	require.Zero(t, stored.ActiveAssignmentID)
	require.NoError(t, s.Assign(ctx, admin, AssignInput{AccountType: AccountTypeOpening, Items: []AssignItem{{ID: opening.ID, Version: 1}}, OperatorID: owner.OperatorID}))
	view, err := s.GetAccount(ctx, owner, opening.ID, AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, AccountTypeOpening, view.AccountType)
	require.Equal(t, "4242", view.CardLast4)
	_, err = s.UploadAttachment(ctx, owner, view.AssignmentID, bytes.NewReader(mailboxTestPNG(t)))
	workflowTestStatus(t, err, 404)
	file, err := s.UploadAttachment(ctx, owner, view.AssignmentID, bytes.NewReader(mailboxTestPNG(t)), AccountTypeOpening)
	require.NoError(t, err)
	_, _, err = s.ReadAttachment(ctx, owner, file.ID)
	workflowTestStatus(t, err, 404)
	_, _, err = s.ReadAttachment(ctx, owner, file.ID, AccountTypeOpening)
	require.NoError(t, err)
	_, err = s.Submit(ctx, owner, view.AssignmentID, view.AssignmentVersion, []string{file.ID})
	workflowTestStatus(t, err, 404)
	submission, err := s.Submit(ctx, owner, view.AssignmentID, view.AssignmentVersion, []string{file.ID}, AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, AccountTypeOpening, submission.AccountType)
	workflowTestStatus(t, s.Review(ctx, admin, submission.ID, ReviewInput{Version: 1, Status: StatusApproved}), 404)
	require.NoError(t, s.Review(ctx, admin, submission.ID, ReviewInput{Version: 1, Status: StatusApproved}, AccountTypeOpening))
	for _, kind := range []string{"password", "otp", "card"} {
		secret, err := s.Credentials(ctx, owner, opening.ID, kind, AccountTypeOpening)
		require.Nil(t, secret)
		workflowTestStatus(t, err, 403)
	}
	history, err := s.ListSubmissions(ctx, owner, ListQuery{})
	require.NoError(t, err)
	require.Zero(t, history.Total)
	history, err = s.ListSubmissions(ctx, owner, ListQuery{AccountType: AccountTypeOpening})
	require.NoError(t, err)
	require.EqualValues(t, 1, history.Total)
	require.Equal(t, AccountTypeOpening, history.Items[0].AccountType)
	view, err = s.GetAccount(ctx, admin, opening.ID, AccountTypeOpening)
	require.NoError(t, err)
	require.NoError(t, s.Assign(ctx, admin, AssignInput{AccountType: AccountTypeOpening, Items: []AssignItem{{ID: opening.ID, Version: view.Version}}, OperatorID: other.OperatorID}))
	secret, err := s.Credentials(ctx, owner, opening.ID, "card", AccountTypeOpening)
	require.Nil(t, secret)
	workflowTestStatus(t, err, 404)
	secret, err = s.Credentials(ctx, other, opening.ID, "card", AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, poolTestPAN, secret.CardNumber)
	view, err = s.GetAccount(ctx, admin, opening.ID, AccountTypeOpening)
	require.NoError(t, err)
	require.NoError(t, s.Assign(ctx, admin, AssignInput{AccountType: AccountTypeOpening, Items: []AssignItem{{ID: opening.ID, Version: view.Version}}, OperatorID: 0}))
	secret, err = s.Credentials(ctx, other, opening.ID, "card", AccountTypeOpening)
	require.Nil(t, secret)
	workflowTestStatus(t, err, 404)
	history, err = s.ListSubmissions(ctx, owner, ListQuery{AccountType: AccountTypeOpening})
	require.NoError(t, err)
	require.EqualValues(t, 1, history.Total)
	_, _, err = s.ReadAttachment(ctx, owner, file.ID, AccountTypeOpening)
	require.NoError(t, err)
	_, _, err = s.ReadAttachment(ctx, other, file.ID, AccountTypeOpening)
	workflowTestStatus(t, err, 404)
}

func TestMailboxPoolGetAccountRejectsStaleAssignmentSnapshot(t *testing.T) {
	for _, change := range []string{"reassign", "approve", "version"} {
		t.Run(change, func(t *testing.T) {
			s, admin, owner, _ := workflowTestService(t)
			account := workflowTestAccount(t, s, "snapshot@example.test")
			view := workflowTestAssign(t, s, admin, owner, account.ID)
			changed := false
			require.NoError(t, s.DB.Callback().Query().Before("gorm:query").Register("pool-detail-snapshot", func(tx *gorm.DB) {
				if changed || tx.Statement.Table != "mailbox_accounts" {
					return
				}
				changed = true
				db := tx.Session(&gorm.Session{NewDB: true})
				switch change {
				case "reassign":
					assignment := model.MailboxAssignment{AccountID: account.ID, OperatorID: owner.OperatorID, Status: StatusPending, Version: 1}
					require.NoError(t, db.Create(&assignment).Error)
					require.NoError(t, db.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Updates(map[string]any{"active_assignment_id": assignment.ID, "version": view.Version + 1}).Error)
				case "approve":
					require.NoError(t, db.Model(&model.MailboxAssignment{}).Where("id = ?", view.AssignmentID).Updates(map[string]any{"status": StatusApproved, "version": view.AssignmentVersion + 1}).Error)
				case "version":
					require.NoError(t, db.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("version", view.Version+1).Error)
				}
			}))
			result, err := s.GetAccount(context.Background(), owner, account.ID)
			require.True(t, changed)
			require.Nil(t, result)
			workflowTestStatus(t, err, 409)
		})
	}
}

func TestMailboxOpeningCredentialsRevalidateAfterAudit(t *testing.T) {
	for _, kind := range []string{"password", "otp", "card"} {
		for _, change := range []string{"approved", "recall", "session", "cipher", "admin"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				s, admin := newMailboxImportTestService(t)
				account := poolTestOpening(t, s, admin, "audit-race@example.test")
				actor := mailboxCredentialTestOperator(t, s, account.ID)
				if change == "admin" {
					actor = admin
				}
				changed := false
				require.NoError(t, s.DB.Callback().Create().After("gorm:create").Register("pool-credential-audit-race", func(tx *gorm.DB) {
					if changed || tx.Statement.Table != "mailbox_audits" {
						return
					}
					changed = true
					db := tx.Session(&gorm.Session{NewDB: true})
					var err error
					switch change {
					case "approved":
						err = db.Model(&model.MailboxAssignment{}).Where("account_id = ?", account.ID).Update("status", StatusApproved).Error
					case "recall":
						err = db.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("active_assignment_id", 0).Error
					case "session":
						err = db.Where("token_hash = ?", actor.SessionHash).Delete(&model.MailboxSession{}).Error
					case "cipher":
						err = db.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("card_ciphertext", "changed-synthetic-ciphertext").Error
					case "admin":
						err = db.Model(&model.User{}).Where("id = ?", admin.Admin.UserID).Update("authorization_version", admin.Admin.Version+1).Error
					}
					require.NoError(t, err)
				}))
				view, err := s.Credentials(context.Background(), actor, account.ID, kind, AccountTypeOpening)
				require.True(t, changed)
				require.Nil(t, view)
				require.Error(t, err)
			})
		}
	}
}
