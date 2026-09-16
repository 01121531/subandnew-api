package mailbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func cvvTestService(t *testing.T) (*Service, Actor, Actor) {
	t.Helper()
	t.Setenv("MAILBOX_TEMP_CVV_MODE", "single_node")
	t.Setenv("NODE_TYPE", "master")
	master := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = master })
	s, admin := newMailboxImportTestService(t)
	op := model.MailboxOperator{Username: "cvv-test", DisplayName: "Test", Enabled: true, AuthVersion: 1, Version: 1}
	require.NoError(t, s.DB.Create(&op).Error)
	hash := digest("synthetic-cvv-session")
	require.NoError(t, s.DB.Create(&model.MailboxSession{OperatorID: op.ID, TokenHash: hash, AuthVersion: 1, ExpiresAt: s.Now().Unix() + 86400}).Error)
	return s, admin, Actor{OperatorID: op.ID, AuthVersion: 1, SessionHash: hash}
}

func cvvTestAssign(t *testing.T, s *Service, admin, operator Actor, id int64) *AccountView {
	t.Helper()
	view, err := s.GetAccount(context.Background(), admin, id, AccountTypeOpening)
	require.NoError(t, err)
	require.NoError(t, s.Assign(context.Background(), admin, AssignInput{AccountType: AccountTypeOpening, Items: []AssignItem{{ID: id, Version: view.Version}}, OperatorID: operator.OperatorID}))
	view, err = s.GetAccount(context.Background(), admin, id, AccountTypeOpening)
	require.NoError(t, err)
	return view
}

func cvvTestImport(t *testing.T, s *Service, admin Actor, value string) model.MailboxAccount {
	t.Helper()
	data := []byte(strings.TrimSpace(string(poolTestCSV("cvv@example.test"))) + "," + value + "\n")
	preview, err := s.PreviewImport(context.Background(), admin, "csv", data, AccountTypeOpening)
	require.NoError(t, err)
	require.True(t, preview.Valid, "%+v", preview.Issues)
	encoded, err := json.Marshal(preview)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), value)
	_, err = s.Import(context.Background(), admin, "csv", data, AccountTypeOpening)
	require.NoError(t, err)
	var account model.MailboxAccount
	require.NoError(t, s.DB.Where("email = ?", "cvv@example.test").First(&account).Error)
	cipher, err := s.Cipher()
	require.NoError(t, err)
	for _, input := range []struct{ kind, version, ciphertext string }{
		{mailboxCredentialKind, account.KeyVersion, account.Ciphertext},
		{mailboxCardCredentialKind, account.CardKeyVersion, account.CardCiphertext},
	} {
		payload, err := cipher.Decrypt(account.ID, input.kind, input.version, input.ciphertext)
		require.NoError(t, err)
		require.NotContains(t, payload.Secret, `"cvv"`)
		require.NotContains(t, payload.Secret, value)
	}
	return account
}

func TestPersistentCVVImportEncryptedAndNoExpiry(t *testing.T) {
	s, admin, operator := cvvTestService(t)
	account := cvvTestImport(t, s, admin, "007")
	cvvTestAssign(t, s, admin, operator, account.ID)
	for range 3 {
		view, err := s.Credentials(context.Background(), admin, account.ID, "cvv", AccountTypeOpening)
		require.NoError(t, err)
		require.Equal(t, "007", view.CVV)
		require.Zero(t, view.ExpiresAt)
	}
	for range 3 {
		view, err := s.Credentials(context.Background(), operator, account.ID, "cvv", AccountTypeOpening)
		require.NoError(t, err)
		require.Equal(t, "007", view.CVV)
	}
	var stored model.MailboxCVV
	require.NoError(t, s.DB.First(&stored, "account_id = ?", account.ID).Error)
	require.NotEqual(t, "007", stored.Ciphertext)
	require.Equal(t, int64(1), stored.Version)
	var audits []model.MailboxAudit
	require.NoError(t, s.DB.Find(&audits).Error)
	encoded, err := json.Marshal(audits)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "007")
}

func TestTemporaryCVVReimportUpdatesCredentialsAndKeepsAssignment(t *testing.T) {
	s, admin, operator := cvvTestService(t)
	account := cvvTestImport(t, s, admin, "007")
	assigned := cvvTestAssign(t, s, admin, operator, account.ID)
	data := bytes.ReplaceAll(poolTestCSV("cvv@example.test"), []byte("synthetic-password"), []byte("new-password"))
	data = []byte(strings.TrimSpace(string(data)) + ",0007")
	updater := s.WithImportUpdateExisting(true)
	preview, err := updater.PreviewImport(context.Background(), admin, "csv", data, AccountTypeOpening)
	require.NoError(t, err)
	require.True(t, preview.Valid, "%+v", preview.Issues)
	result, err := updater.Import(context.Background(), admin, "csv", data, AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, 1, result.Imported)
	require.Equal(t, 1, result.Updated)
	view, err := s.GetAccount(context.Background(), operator, account.ID, AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, assigned.AssignmentID, view.AssignmentID)
	cvv, err := s.Credentials(context.Background(), operator, account.ID, "cvv", AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, "0007", cvv.CVV)
	password, err := s.Credentials(context.Background(), operator, account.ID, "password", AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, "new-password", password.Password)
}

func TestTemporaryCVVReassignmentAndDisableInvalidate(t *testing.T) {
	for _, operation := range []string{"reassign", "recall", "disable", "restart"} {
		t.Run(operation, func(t *testing.T) {
			s, admin, operator := cvvTestService(t)
			account := cvvTestImport(t, s, admin, "0012")
			cvvTestAssign(t, s, admin, operator, account.ID)
			switch operation {
			case "reassign":
				cvvTestAssign(t, s, admin, operator, account.ID)
			case "recall":
				cvvTestAssign(t, s, admin, Actor{}, account.ID)
			case "disable":
				_, err := s.SaveOperator(context.Background(), admin, operator.OperatorID, OperatorInput{Username: "cvv-test", DisplayName: "Test", Version: 1, Enabled: false})
				require.NoError(t, err)
			case "restart":
				restarted := New(s.DB)
				restarted.Cipher, restarted.Now = s.Cipher, s.Now
				s = restarted
			}
			_, err := s.Credentials(context.Background(), operator, account.ID, "cvv", AccountTypeOpening)
			if operation == "restart" || operation == "reassign" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			var stored model.MailboxCVV
			require.NoError(t, s.DB.First(&stored, "account_id = ?", account.ID).Error)
		})
	}
}

func TestPersistentCVVDoesNotExpireAfterFirstView(t *testing.T) {
	s, admin, operator := cvvTestService(t)
	account := cvvTestImport(t, s, admin, "0012")
	cvvTestAssign(t, s, admin, operator, account.ID)
	base := s.Now()
	s.Now = func() time.Time { return base.Add(12 * time.Hour) }
	first, err := s.Credentials(context.Background(), operator, account.ID, "cvv", AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, "0012", first.CVV)
	require.Zero(t, first.ExpiresAt)
	require.True(t, first.Persistent)
	s.Now = func() time.Time { return base.Add(13 * time.Hour) }
	second, err := s.Credentials(context.Background(), operator, account.ID, "cvv", AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, first.ExpiresAt, second.ExpiresAt)
	s.Now = func() time.Time { return base.Add(400 * 24 * time.Hour) }
	last, err := s.Credentials(context.Background(), admin, account.ID, "cvv", AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, "0012", last.CVV)
}

func TestTemporaryCVVValidationAndFeatureGate(t *testing.T) {
	s, admin, _ := cvvTestService(t)
	for i, value := range []string{"", "12", "12345", " 123", "12x", "１２３"} {
		data := strings.TrimSpace(string(poolTestCSV(fmt.Sprintf("invalid%d@example.test", i)))) + "," + value
		_, err := s.Import(context.Background(), admin, "csv", []byte(data), AccountTypeOpening)
		require.Error(t, err)
	}
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Count(&count).Error)
	require.Zero(t, count)
	t.Setenv("MAILBOX_TEMP_CVV_MODE", "disabled")
	_, err := s.Import(context.Background(), admin, "csv", []byte(strings.TrimSpace(string(poolTestCSV("disabled@example.test")))+",007"), AccountTypeOpening)
	require.Error(t, err)
	_, err = s.Import(context.Background(), admin, "csv", poolTestCSV("five-columns@example.test"), AccountTypeOpening)
	require.NoError(t, err)
}

func TestMailboxActionablePagination(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	for i, status := range []string{StatusPending, StatusApproved, StatusRejected, StatusSubmitted} {
		account := workflowTestAccount(t, s, fmt.Sprintf("queue%d@example.test", i))
		view := workflowTestAssign(t, s, admin, owner, account.ID)
		require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", view.AssignmentID).Update("status", status).Error)
	}
	page, err := s.ListAccounts(context.Background(), owner, ListQuery{Status: "actionable", Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.True(t, page.HasMore)
	require.Equal(t, StatusRejected, page.Items[0].Status)
	next, err := s.ListAccounts(context.Background(), owner, ListQuery{Status: "actionable", Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.False(t, next.HasMore)
	require.Equal(t, StatusPending, next.Items[0].Status)
}

func TestTemporaryCVVDeliveryMetadataAndRefill(t *testing.T) {
	s, admin, operator := cvvTestService(t)
	account := cvvTestImport(t, s, admin, "007")
	cvvTestAssign(t, s, admin, operator, account.ID)
	view, err := s.GetAccount(context.Background(), operator, account.ID, AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, "available", view.TemporaryCVVStatus)
	require.NotEmpty(t, view.TemporaryCVVID)
	firstID := view.TemporaryCVVID
	_, err = s.Credentials(context.Background(), operator, account.ID, "cvv", AccountTypeOpening)
	require.NoError(t, err)
	view, err = s.GetAccount(context.Background(), operator, account.ID, AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, firstID, view.TemporaryCVVID)
	require.Equal(t, "available", view.TemporaryCVVStatus)
	_, err = s.ProvideTemporaryCVV(context.Background(), admin, account.ID, TemporaryCVVInput{Version: view.Version, CVV: "000"}, AccountTypeOpening)
	require.NoError(t, err)
	view, err = s.GetAccount(context.Background(), operator, account.ID, AccountTypeOpening)
	require.NoError(t, err)
	require.NotEmpty(t, view.TemporaryCVVID)
	require.NotEqual(t, firstID, view.TemporaryCVVID)
	adminView, err := s.GetAccount(context.Background(), admin, account.ID, AccountTypeOpening)
	require.NoError(t, err)
	require.Empty(t, adminView.TemporaryCVVID)
}

func TestPersistentCVVClearVersionAndReimportOmission(t *testing.T) {
	s, admin, _ := cvvTestService(t)
	account := cvvTestImport(t, s, admin, "007")
	_, err := s.WithImportUpdateExisting(true).Import(context.Background(), admin, "csv", poolTestCSV(account.Email), AccountTypeOpening)
	require.NoError(t, err)
	value, err := s.Credentials(context.Background(), admin, account.ID, "cvv", AccountTypeOpening)
	require.NoError(t, err)
	require.Equal(t, "007", value.CVV)
	workflowTestStatus(t, s.ClearCVV(context.Background(), admin, account.ID, TemporaryCVVInput{Version: account.Version}, AccountTypeOpening), 409)
	view, err := s.GetAccount(context.Background(), admin, account.ID, AccountTypeOpening)
	require.NoError(t, err)
	require.NoError(t, s.ClearCVV(context.Background(), admin, account.ID, TemporaryCVVInput{Version: view.Version}, AccountTypeOpening))
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxCVV{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestTemporaryCVVSixColumnFormatsAndNumericCellRejection(t *testing.T) {
	for _, format := range []string{"text", "csv", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			s, admin, operator := cvvTestService(t)
			cells := []string{"formats@example.test", "  unchanged  ", mailboxImportTestSecret, poolTestPAN, "12/99", "0007"}
			var data []byte
			switch format {
			case "text":
				data = []byte(strings.Join(cells, "----"))
			case "csv":
				data = []byte("email,password,2fa,card_number,card_expiry,CVV\n" + strings.Join(cells, ","))
			case "xlsx":
				data = mailboxImportTestWorkbook(t, [][]string{cells}, "")
			}
			preview, err := s.PreviewImport(context.Background(), admin, format, data, AccountTypeOpening)
			require.NoError(t, err)
			require.True(t, preview.Valid, "%+v", preview.Issues)
			_, err = s.Import(context.Background(), admin, format, data, AccountTypeOpening)
			require.NoError(t, err)
			var account model.MailboxAccount
			require.NoError(t, s.DB.First(&account).Error)
			cvvTestAssign(t, s, admin, operator, account.ID)
			value, err := s.Credentials(context.Background(), operator, account.ID, "cvv", AccountTypeOpening)
			require.NoError(t, err)
			require.Equal(t, "0007", value.CVV)
		})
	}
	s, admin, _ := cvvTestService(t)
	book := excelize.NewFile()
	defer book.Close()
	row := []any{"numeric@example.test", "synthetic", mailboxImportTestSecret, poolTestPAN, "12/99", 123}
	require.NoError(t, book.SetSheetRow("Sheet1", "A1", &row))
	buffer, err := book.WriteToBuffer()
	require.NoError(t, err)
	preview, err := s.PreviewImport(context.Background(), admin, "xlsx", buffer.Bytes(), AccountTypeOpening)
	require.NoError(t, err)
	require.False(t, preview.Valid)
	encoded, err := json.Marshal(preview)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "mailbox_import_cvv_text_required")
}
