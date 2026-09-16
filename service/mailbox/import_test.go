package mailbox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const mailboxImportTestSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func newMailboxImportTestService(t *testing.T) (*Service, Actor) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mailbox.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, db.AutoMigrate(&model.MailboxIssue{}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AdminDataPolicy{}, &model.MailboxAccount{}, &model.MailboxAssignment{}, &model.MailboxOperator{}, &model.MailboxSession{}, &model.MailboxAudit{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "mailbox-import-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}).Error)
	access, err := authz.LoadDataAccess(db, 1)
	require.NoError(t, err)
	cipher, err := managedinstance.NewCredentialCipher(bytes.Repeat([]byte{0x5a}, 32), "mailbox-test-v1")
	require.NoError(t, err)
	s := New(db)
	s.Now = func() time.Time { return time.Unix(59, 0) }
	s.Cipher = func() (*managedinstance.CredentialCipher, error) { return cipher, nil }
	return s, Actor{Admin: access}
}

func mailboxImportTestAccount(t *testing.T, s *Service, actor Actor, email, password, secret string) model.MailboxAccount {
	t.Helper()
	var data bytes.Buffer
	writer := csv.NewWriter(&data)
	require.NoError(t, writer.Write([]string{email, password, secret}))
	writer.Flush()
	require.NoError(t, writer.Error())
	result, err := s.Import(context.Background(), actor, "csv", data.Bytes())
	require.NoError(t, err)
	require.Equal(t, 1, result.Imported)
	var account model.MailboxAccount
	require.NoError(t, s.DB.Where("email = ?", strings.ToLower(email)).First(&account).Error)
	return account
}

func mailboxImportTestWorkbook(t *testing.T, cells [][]string, formula string) []byte {
	t.Helper()
	book := excelize.NewFile()
	t.Cleanup(func() { _ = book.Close() })
	for row, values := range cells {
		for col, value := range values {
			cell, err := excelize.CoordinatesToCellName(col+1, row+1)
			require.NoError(t, err)
			require.NoError(t, book.SetCellStr("Sheet1", cell, value))
		}
	}
	if formula != "" {
		require.NoError(t, book.SetCellFormula("Sheet1", "B2", formula))
	}
	data, err := book.WriteToBuffer()
	require.NoError(t, err)
	return data.Bytes()
}

func TestMailboxImportFormatsPreservePasswordsAndRedact(t *testing.T) {
	password := "  sensitive,\"password\"  "
	var csvData bytes.Buffer
	writer := csv.NewWriter(&csvData)
	require.NoError(t, writer.WriteAll([][]string{{"email", "password", "2fa"}, {"First.Last+Tag@Example.COM", password, mailboxImportTestSecret}}))
	cases := []struct {
		name, format string
		data         []byte
	}{
		{"tabs", "text", []byte(" First.Last+Tag@Example.COM \t" + password + "\t" + mailboxImportTestSecret + "\r\n")},
		{"dashes", "text", []byte("First.Last+Tag@Example.COM----" + password + "----" + mailboxImportTestSecret)},
		{"csv", "csv", csvData.Bytes()},
		{"xlsx", "xlsx", mailboxImportTestWorkbook(t, [][]string{{"email", "password", "2fa"}, {"First.Last+Tag@Example.COM", password, mailboxImportTestSecret}}, "")},
		{"localized", "csv", []byte("\ufeff\u90ae\u7bb1,\u5bc6\u7801,\u4e24\u6b65\u9a8c\u8bc1\nFirst.Last+Tag@Example.COM,  secret  ," + mailboxImportTestSecret)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, actor := newMailboxImportTestService(t)
			preview, err := s.PreviewImport(context.Background(), actor, tc.format, tc.data)
			require.NoError(t, err)
			require.True(t, preview.Valid)
			require.Equal(t, 1, preview.Total)
			require.Equal(t, "first.last+tag@example.com", preview.Rows[0].Email)
			encoded, err := json.Marshal(preview)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "sensitive")
			require.NotContains(t, string(encoded), mailboxImportTestSecret)
			result, err := s.Import(context.Background(), actor, tc.format, tc.data)
			require.NoError(t, err)
			require.Equal(t, 1, result.Imported)
			var account model.MailboxAccount
			require.NoError(t, s.DB.First(&account).Error)
			require.NotEmpty(t, account.Ciphertext)
			require.NotContains(t, account.Ciphertext, "sensitive")
			require.NotContains(t, account.Ciphertext, mailboxImportTestSecret)
			view, err := s.Credentials(context.Background(), actor, account.ID, "password")
			require.NoError(t, err)
			want := password
			if tc.name == "localized" {
				want = "  secret  "
			}
			require.Equal(t, want, view.Password)
			require.Empty(t, view.Code)
			var audits []model.MailboxAudit
			require.NoError(t, s.DB.Find(&audits).Error)
			encoded, err = json.Marshal(audits)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "sensitive")
			require.NotContains(t, string(encoded), mailboxImportTestSecret)
		})
	}
}

func TestMailboxImportOpeningAllowsExplicitNoOTPMarker(t *testing.T) {
	t.Setenv("MAILBOX_TEMP_CVV_MODE", "single_node")
	s, actor, _ := cvvTestService(t)
	data := []byte("dwv55euhn2@chenhannb.us----weU4qZKE----XXXX----4565991041326975----09/2031----721")

	preview, err := s.PreviewImport(context.Background(), actor, "text", data, AccountTypeOpening)
	require.NoError(t, err)
	require.True(t, preview.Valid, "%+v", preview.Issues)
	require.Equal(t, 1, preview.Ready)
	require.Len(t, preview.Rows, 1)
	require.False(t, preview.Rows[0].OTPAvailable)
	require.Contains(t, preview.Notices, ImportIssue{Row: 1, Code: "mailbox_import_otp_absent"})
	encoded, err := json.Marshal(preview)
	require.NoError(t, err)
	for _, secret := range []string{"weU4qZKE", "4565991041326975", "721"} {
		require.NotContains(t, string(encoded), secret)
	}

	result, err := s.Import(context.Background(), actor, "text", data, AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, 1, result.Imported)
	var account model.MailboxAccount
	require.NoError(t, s.DB.First(&account).Error)
	require.NotContains(t, account.Ciphertext, mailboxOTPAbsentMarker)
	password, err := s.Credentials(context.Background(), actor, account.ID, "password", AccountTypeOpening)
	require.NoError(t, err)
	require.True(t, password.Available)
	require.Equal(t, "weU4qZKE", password.Password)
	otp, err := s.Credentials(context.Background(), actor, account.ID, "otp", AccountTypeOpening)
	require.NoError(t, err)
	require.False(t, otp.Available)
	require.Empty(t, otp.Code)
	require.Zero(t, otp.ExpiresAt)
	card, err := s.Credentials(context.Background(), actor, account.ID, "card", AccountTypeOpening)
	require.NoError(t, err)
	require.True(t, card.Available)
	require.Equal(t, "4565991041326975", card.CardNumber)
	require.Equal(t, "09/31", card.CardExpiry)
}

func TestMailboxImportNoOTPMarkerIsExplicit(t *testing.T) {
	for _, marker := range []string{"XXXX", "xxxx", "  XxXx  "} {
		t.Run(marker, func(t *testing.T) {
			s, actor := newMailboxImportTestService(t)
			preview, err := s.PreviewImport(context.Background(), actor, "text", []byte("no-otp@example.test----password----"+marker))
			require.NoError(t, err)
			require.True(t, preview.Valid)
			require.False(t, preview.Rows[0].OTPAvailable)
		})
	}
	for _, marker := range []string{"", "XXX", "XXXXX", "NONE"} {
		t.Run("invalid-"+marker, func(t *testing.T) {
			s, actor := newMailboxImportTestService(t)
			preview, err := s.PreviewImport(context.Background(), actor, "text", []byte("invalid@example.test----password----"+marker))
			require.NoError(t, err)
			require.False(t, preview.Valid)
			require.Contains(t, preview.Issues, ImportIssue{Row: 1, Code: "mailbox_import_invalid_otp"})
		})
	}
}

func TestMailboxImportInvalidRowsAndAtomicValidation(t *testing.T) {
	cases := []struct{ name, input, code string }{
		{"empty", "", "mailbox_import_empty"},
		{"null", "null", "mailbox_import_columns"},
		{"email", "not-email\tpw\t" + mailboxImportTestSecret, "mailbox_import_invalid_email"},
		{"display name", "Name <one@example.com>\tpw\t" + mailboxImportTestSecret, "mailbox_import_invalid_email"},
		{"password", "one@example.com\t\t" + mailboxImportTestSecret, "mailbox_import_invalid_password"},
		{"otp", "one@example.com\tpw\t ", "mailbox_import_invalid_otp"},
		{"extra", "one@example.com----pw----extra----" + mailboxImportTestSecret, "mailbox_import_columns"},
		{"mixed", "one@example.com\tpw----extra\t" + mailboxImportTestSecret, "mailbox_import_columns"},
		{"duplicate", "one@example.com\tpw\t" + mailboxImportTestSecret + "\nONE@EXAMPLE.COM\tother\t" + mailboxImportTestSecret, "mailbox_import_duplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, actor := newMailboxImportTestService(t)
			preview, err := s.PreviewImport(context.Background(), actor, "text", []byte(tc.input))
			require.NoError(t, err)
			require.False(t, preview.Valid)
			codes := []string{}
			for _, issue := range preview.Issues {
				codes = append(codes, issue.Code)
			}
			require.Contains(t, codes, tc.code)
			_, err = s.Import(context.Background(), actor, "text", []byte("valid@example.com\tgood\t"+mailboxImportTestSecret+"\n"+tc.input))
			if tc.input != "" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			var count int64
			require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Count(&count).Error)
			if tc.input != "" {
				require.Zero(t, count)
			}
		})
	}
}

func TestMailboxImportRejectsIllegalFormatsAndLimits(t *testing.T) {
	s, actor := newMailboxImportTestService(t)
	cases := []struct {
		name, format, code string
		data               []byte
	}{
		{"format", "json", "mailbox_import_invalid_format", []byte("null")},
		{"nil xlsx", "xlsx", "mailbox_import_invalid_xlsx", nil},
		{"invalid xlsx", "xlsx", "mailbox_import_invalid_xlsx", []byte("not a workbook")},
		{"malformed csv", "csv", "mailbox_import_invalid_csv", []byte("one@example.com,\"secret," + mailboxImportTestSecret)},
		{"nul", "text", "mailbox_import_invalid_encoding", []byte("one@example.com\tpw\x00\t" + mailboxImportTestSecret)},
		{"utf8", "csv", "mailbox_import_invalid_encoding", []byte{0xff}},
		{"size", "text", "mailbox_import_too_large", bytes.Repeat([]byte{'a'}, mailboxImportMaxBytes+1)},
		{"rows", "text", "mailbox_import_too_many_rows", []byte(strings.Repeat("one@example.com\tpw\t"+mailboxImportTestSecret+"\n", 1001))},
		{"formula", "xlsx", "mailbox_import_formula", mailboxImportTestWorkbook(t, [][]string{{"email", "password", "2fa"}, {"one@example.com", "pw", mailboxImportTestSecret}}, "1+1")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			preview, err := s.PreviewImport(context.Background(), actor, tc.format, tc.data)
			require.Nil(t, preview)
			require.EqualError(t, err, tc.code)
			result, err := s.Import(context.Background(), actor, tc.format, tc.data)
			require.Nil(t, result)
			require.EqualError(t, err, tc.code)
		})
	}
}

func TestMailboxImportDatabaseDuplicatesNoOverwrite(t *testing.T) {
	s, actor := newMailboxImportTestService(t)
	account := mailboxImportTestAccount(t, s, actor, "one@example.com", "original", mailboxImportTestSecret)
	data := []byte("new@example.com\tnew-password\t" + mailboxImportTestSecret + "\nONE@EXAMPLE.COM\treplacement\t" + mailboxImportTestSecret)
	preview, err := s.PreviewImport(context.Background(), actor, "text", data)
	require.NoError(t, err)
	require.False(t, preview.Valid)
	require.Contains(t, preview.Issues, ImportIssue{Row: 2, Code: "mailbox_import_exists"})
	_, err = s.Import(context.Background(), actor, "text", data)
	require.Error(t, err)
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
	view, err := s.Credentials(context.Background(), actor, account.ID, "password")
	require.NoError(t, err)
	require.Equal(t, "original", view.Password)
}

func TestMailboxImportRollbackOnMidWriteAndAuditFailure(t *testing.T) {
	for _, failure := range []string{"update", "audit", "cipher", "conflict"} {
		t.Run(failure, func(t *testing.T) {
			s, actor := newMailboxImportTestService(t)
			switch failure {
			case "update":
				updates := 0
				require.NoError(t, s.DB.Callback().Update().Before("gorm:update").Register("mailbox_import_test_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "mailbox_accounts" {
						updates++
						if updates == 2 {
							tx.AddError(errors.New("private database failure"))
						}
					}
				}))
			case "audit":
				require.NoError(t, s.DB.Callback().Create().Before("gorm:create").Register("mailbox_import_test_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "mailbox_audits" {
						tx.AddError(errors.New("private audit failure"))
					}
				}))
			case "cipher":
				s.Cipher = func() (*managedinstance.CredentialCipher, error) { return nil, nil }
			case "conflict":
				original := s.Cipher
				s.Cipher = func() (*managedinstance.CredentialCipher, error) {
					require.NoError(t, s.DB.Create(&model.MailboxAccount{Email: "two@example.com", Ciphertext: "existing", KeyVersion: "existing", Version: 1}).Error)
					return original()
				}
			}
			data := []byte("one@example.com\tpw\t" + mailboxImportTestSecret + "\ntwo@example.com\tpw\t" + mailboxImportTestSecret)
			result, err := s.Import(context.Background(), actor, "text", data)
			require.Error(t, err)
			require.Nil(t, result)
			require.NotContains(t, err.Error(), "private")
			var accounts []model.MailboxAccount
			require.NoError(t, s.DB.Find(&accounts).Error)
			if failure == "conflict" {
				require.EqualError(t, err, "mailbox_import_conflict")
				require.Len(t, accounts, 1)
				require.Equal(t, "existing", accounts[0].Ciphertext)
			} else {
				require.Empty(t, accounts)
			}
		})
	}
}

func TestMailboxImportThousandRows(t *testing.T) {
	s, actor := newMailboxImportTestService(t)
	var data strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&data, "user%d@example.com\tpw\t%s\n", i, mailboxImportTestSecret)
	}
	result, err := s.Import(context.Background(), actor, "text", []byte(data.String()))
	require.NoError(t, err)
	require.Equal(t, 1000, result.Imported)
}

func TestMailboxImportCSVPreservesMultilinePasswordBytes(t *testing.T) {
	s, actor := newMailboxImportTestService(t)
	data := []byte("\r\nemail,password,2fa\r\n\r\none@example.com,\" first\r\nline,\"\"quoted\"\"\nlast \r\n\"," + mailboxImportTestSecret + "\r\n\ntwo@example.com,\" second\r\npassword \"," + mailboxImportTestSecret)
	preview, err := s.PreviewImport(context.Background(), actor, "csv", data)
	require.NoError(t, err)
	require.True(t, preview.Valid)
	require.Equal(t, 4, preview.Rows[0].Row)
	result, err := s.Import(context.Background(), actor, "csv", data)
	require.NoError(t, err)
	require.Equal(t, 2, result.Imported)
	for email, password := range map[string]string{"one@example.com": " first\r\nline,\"quoted\"\nlast \r\n", "two@example.com": " second\r\npassword "} {
		var account model.MailboxAccount
		require.NoError(t, s.DB.Where("email = ?", email).First(&account).Error)
		view, err := s.Credentials(context.Background(), actor, account.ID, "password")
		require.NoError(t, err)
		require.Equal(t, password, view.Password)
	}
}

func TestMailboxCSVPasswordRoundTrip(t *testing.T) {
	parts := []string{"", " ", "plain", "\r", "\n", "\r\n", "\"", ",", "\u5bc6\u7801"}
	for _, left := range parts {
		for _, right := range parts {
			password := left + "middle" + right
			var data bytes.Buffer
			writer := csv.NewWriter(&data)
			require.NoError(t, writer.Write([]string{"name\r\nwith line", password, mailboxImportTestSecret}))
			writer.Flush()
			require.NoError(t, writer.Error())
			reader := csv.NewReader(bytes.NewReader(data.Bytes()))
			_, err := reader.Read()
			require.NoError(t, err)
			require.Equal(t, password, mailboxCSVPassword(data.Bytes(), 1, reader))
		}
	}
}

func TestMailboxXLSXRejectsEmptyFormulaAndUnzipBomb(t *testing.T) {
	for _, mode := range []string{"empty-formula", "unzip-size"} {
		t.Run(mode, func(t *testing.T) {
			var buffer bytes.Buffer
			archive := zip.NewWriter(&buffer)
			entry, err := archive.Create("xl/worksheets/sheet1.xml")
			require.NoError(t, err)
			if mode == "empty-formula" {
				_, err = io.WriteString(entry, `<worksheet><sheetData><row r="2"><c r="B2"><f t="shared" si="0"/><v>cached-password</v></c></row></sheetData></worksheet>`)
			} else {
				_, err = io.Copy(entry, io.LimitReader(strings.NewReader(strings.Repeat("x", mailboxImportUnzipBytes+1)), mailboxImportUnzipBytes+1))
			}
			require.NoError(t, err)
			require.NoError(t, archive.Close())
			require.Less(t, buffer.Len(), mailboxImportMaxBytes)
			err = readMailboxXLSX(buffer.Bytes(), func(int, []string, bool) error { t.Fatal("unsafe workbook reached row parser"); return nil })
			if mode == "empty-formula" {
				require.EqualError(t, err, "mailbox_import_formula")
			} else {
				require.EqualError(t, err, "mailbox_import_too_large")
			}
		})
	}
}
