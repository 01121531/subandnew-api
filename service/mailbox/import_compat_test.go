package mailbox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestMailboxImportCompatibility(t *testing.T) {
	for _, format := range []string{"xlsx", "csv", "text"} {
		t.Run(format, func(t *testing.T) {
			s, actor := newMailboxImportTestService(t)
			lines := []string{
				"first@example.test----  password  ----" + mailboxImportTestSecret,
				"second@example.test----  password  ----recovery@example.test----" + mailboxImportTestSecret,
				"third@example.test----  password  ----https://offline.invalid/code#" + mailboxImportTestSecret,
			}
			data := []byte(strings.Join(lines, "\n"))
			if format == "xlsx" {
				data = mailboxImportTestWorkbook(t, [][]string{{lines[0]}, {}, {lines[1]}, {lines[2]}}, "")
			}
			preview, err := s.PreviewImport(context.Background(), actor, format, data)
			require.NoError(t, err)
			require.True(t, preview.Valid, "%+v", preview.Issues)
			require.Equal(t, 3, preview.Total)
			require.Contains(t, preview.Notices, ImportIssue{Row: preview.Rows[1].Row, Code: "mailbox_import_recovery_email_ignored"})
			require.Contains(t, preview.Notices, ImportIssue{Row: preview.Rows[2].Row, Code: "mailbox_import_otp_link_extracted"})
			encoded, err := json.Marshal(preview)
			require.NoError(t, err)
			for _, secret := range []string{"password", mailboxImportTestSecret, "recovery@example.test", "offline.invalid"} {
				require.NotContains(t, string(encoded), secret)
			}
			result, err := s.Import(context.Background(), actor, format, data)
			require.NoError(t, err)
			require.Equal(t, 3, result.Imported)
			var accounts []model.MailboxAccount
			require.NoError(t, s.DB.Find(&accounts).Error)
			for _, account := range accounts {
				credentials, err := s.Credentials(context.Background(), actor, account.ID, "password")
				require.NoError(t, err)
				require.Equal(t, "  password  ", credentials.Password)
			}
		})
	}
}

func TestMailboxImportCompatibilityRejectsAmbiguousRowsAtomically(t *testing.T) {
	for _, value := range []string{
		"bad@example.test----password----recovery@example.test",
		"bad@example.test----password----" + mailboxImportTestSecret + "----cookie=value",
		"bad@example.test----password----https://offline.invalid/no-secret",
		"bad@example.test----password----https://offline.invalid/?token=value#" + mailboxImportTestSecret,
		"bad@example.test----password----https://user:password@offline.invalid/#" + mailboxImportTestSecret,
		"bad@example.test----password----http://offline.invalid/#" + mailboxImportTestSecret,
		"bad@example.test----password\t----" + mailboxImportTestSecret,
		"bad@example.test----password",
	} {
		t.Run("invalid", func(t *testing.T) {
			s, actor := newMailboxImportTestService(t)
			data := mailboxImportTestWorkbook(t, [][]string{{"good@example.test----password----" + mailboxImportTestSecret}, {value}}, "")
			preview, err := s.PreviewImport(context.Background(), actor, "xlsx", data)
			require.NoError(t, err)
			require.False(t, preview.Valid)
			require.Equal(t, 2, preview.Issues[0].Row)
			_, err = s.Import(context.Background(), actor, "xlsx", data)
			require.Error(t, err)
			var count int64
			require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestMailboxImportPackedOpeningAndHeaders(t *testing.T) {
	s, actor := newMailboxImportTestService(t)
	data := mailboxImportTestWorkbook(t, [][]string{
		{"email\tpassword\t2fa\tcard_number\texpiry"},
		{"opening@example.test\t password \t" + mailboxImportTestSecret + "\t4111111111111111\t12/39"},
	}, "")
	preview, err := s.PreviewImport(context.Background(), actor, "xlsx", data, AccountTypeOpening)
	require.NoError(t, err)
	require.True(t, preview.Valid, "%+v", preview.Issues)
	require.Equal(t, 1, preview.Total)
	require.Equal(t, "1111", preview.Rows[0].CardLast4)
	_, err = s.Import(context.Background(), actor, "xlsx", data, AccountTypeOpening)
	require.NoError(t, err)
}

func TestMailboxImportCompatibilityKeepsDuplicateAndExtraFieldChecks(t *testing.T) {
	s, actor := newMailboxImportTestService(t)
	data := mailboxImportTestWorkbook(t, [][]string{
		{"SAME@example.test----password----" + mailboxImportTestSecret},
		{"same@example.test----password----recovery@example.test----" + mailboxImportTestSecret},
		{"other@example.test----password----" + mailboxImportTestSecret + "----cookie----token"},
	}, "")
	preview, err := s.PreviewImport(context.Background(), actor, "xlsx", data)
	require.NoError(t, err)
	require.False(t, preview.Valid)
	require.Contains(t, preview.Issues, ImportIssue{Row: 1, Code: "mailbox_import_duplicate"})
	require.Contains(t, preview.Issues, ImportIssue{Row: 2, Code: "mailbox_import_duplicate"})
	require.Contains(t, preview.Issues, ImportIssue{Row: 3, Code: "mailbox_import_columns"})
}
