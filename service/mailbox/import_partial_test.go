package mailbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMailboxImportPartialValidRowsAndFailureReport(t *testing.T) {
	for _, format := range []string{"text", "csv", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			s, actor := newMailboxImportTestService(t)
			lines := []string{
				"good@example.test---- original password ----" + mailboxImportTestSecret,
				"missing@example.test----private-password",
				"bad@example.test----private-password----invalid-secret!",
				"duplicate@example.test----pw----" + mailboxImportTestSecret,
				"DUPLICATE@example.test----pw----" + mailboxImportTestSecret,
			}
			data := []byte(strings.Join(lines, "\n"))
			if format == "xlsx" {
				data = mailboxImportTestWorkbook(t, [][]string{{lines[0]}, {lines[1]}, {lines[2]}, {lines[3]}, {lines[4]}}, "")
			}
			preview, err := s.PreviewImport(context.Background(), actor, format, data)
			require.NoError(t, err)
			require.Equal(t, 1, preview.Ready)
			require.Len(t, preview.Failures, 4)
			require.Equal(t, "missing@example.test", preview.Failures[0].Email)
			_, err = s.Import(context.Background(), actor, format, data)
			require.Error(t, err, "legacy requests stay atomic")
			result, err := s.WithPartialImport(true).Import(context.Background(), actor, format, data)
			require.NoError(t, err)
			require.Equal(t, 1, result.Imported)
			require.Equal(t, 4, result.Failed)
			require.Equal(t, preview.Failures, result.Failures)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			for _, secret := range []string{"original password", "private-password", "invalid-secret!", mailboxImportTestSecret} {
				require.NotContains(t, string(encoded), secret)
			}
			var account model.MailboxAccount
			require.NoError(t, s.DB.First(&account).Error)
			credentials, err := s.Credentials(context.Background(), actor, account.ID, "password")
			require.NoError(t, err)
			require.Equal(t, " original password ", credentials.Password)
			// Retrying a completed or uncertain request never overwrites an account.
			result, err = s.WithPartialImport(true).Import(context.Background(), actor, format, data)
			require.NoError(t, err)
			require.Zero(t, result.Imported)
			require.Equal(t, 5, result.Failed)
			var count int64
			require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestMailboxImportPartialOpeningAndFatalErrors(t *testing.T) {
	s, actor := newMailboxImportTestService(t)
	s = s.WithPartialImport(true)
	data := mailboxImportTestWorkbook(t, [][]string{
		{"email", "password", "2fa", "card_number", "expiry"},
		{"valid@example.test", "pw", mailboxImportTestSecret, "4111111111111111", "12/39"},
		{"invalid@example.test", "pw", mailboxImportTestSecret, "4111111111111112", "12/39"},
	}, "")
	result, err := s.Import(context.Background(), actor, "xlsx", data, AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, 1, result.Imported)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, "invalid@example.test", result.Failures[0].Email)
	_, err = s.Import(context.Background(), actor, "csv", []byte("\"unclosed"))
	require.Error(t, err)
	_, err = s.Import(context.Background(), actor, "text", nil)
	require.Error(t, err)
}

func TestMailboxImportPartialRechecksConflictAtCommit(t *testing.T) {
	for _, archived := range []int64{0, 59} {
		s, actor := newMailboxImportTestService(t)
		cipher := s.Cipher
		// A competing writer inserts after preview validation, before this transaction.
		s.Cipher = func() (*managedinstance.CredentialCipher, error) {
			require.NoError(t, s.DB.Create(&model.MailboxAccount{AccountType: AccountTypeRefund, Email: "race@example.test", Version: 1, Ciphertext: "unchanged", ArchivedAt: archived}).Error)
			return cipher()
		}
		data := []byte("race@example.test----pw----" + mailboxImportTestSecret + "\nnew@example.test----pw----" + mailboxImportTestSecret)
		result, err := s.WithPartialImport(true).Import(context.Background(), actor, "text", data)
		require.NoError(t, err)
		require.Equal(t, 1, result.Imported)
		require.Equal(t, 1, result.Failed)
		code := "mailbox_import_conflict"
		if archived != 0 {
			code = "mailbox_import_archived"
		}
		require.Equal(t, []ImportFailure{{Row: 1, Email: "race@example.test", Codes: []string{code}}}, result.Failures)
		var existing model.MailboxAccount
		require.NoError(t, s.DB.Where("email = ?", "race@example.test").First(&existing).Error)
		require.Equal(t, "unchanged", existing.Ciphertext)
	}
}

func TestMailboxImportPartialInfrastructureFailureRollsBack(t *testing.T) {
	s, actor := newMailboxImportTestService(t)
	updates := 0
	require.NoError(t, s.DB.Callback().Update().After("gorm:update").Register("test:fail_import", func(tx *gorm.DB) {
		if tx.Statement.Table == "mailbox_accounts" {
			updates++
			if updates == 2 {
				_ = tx.AddError(errors.New("simulated storage failure"))
			}
		}
	}))
	data := []byte("first@example.test----pw----" + mailboxImportTestSecret + "\nsecond@example.test----pw----" + mailboxImportTestSecret + "\nbad@example.test----pw")
	result, err := s.WithPartialImport(true).Import(context.Background(), actor, "text", data)
	require.Error(t, err)
	require.Nil(t, result)
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Count(&count).Error)
	require.Zero(t, count)
}
