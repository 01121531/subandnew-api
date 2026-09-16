package mailbox

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func completedTestSetup(t *testing.T) (*Service, Actor, Actor) {
	t.Helper()
	s, admin, owner := cvvTestService(t)
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxSubmission{}, &model.MailboxAttachment{}))
	s.Now = func() time.Time { return time.Date(2026, 9, 16, 17, 2, 3, 0, time.UTC) }
	require.NoError(t, s.DB.Model(&model.MailboxSession{}).Where("operator_id = ?", owner.OperatorID).Update("expires_at", s.Now().Unix()+86400).Error)
	return s, admin, owner
}

func completedTestSubmit(t *testing.T, s *Service, admin, owner Actor, email string) (model.MailboxAccount, *SubmissionView) {
	t.Helper()
	a := poolTestOpening(t, s, admin, email)
	v := cvvTestAssign(t, s, admin, owner, a.ID)
	u, err := s.SubmitWithRemark(t.Context(), owner, v.AssignmentID, v.AssignmentVersion, nil, "=1+1\n完成", AccountTypeOpening)
	require.NoError(t, err)
	return a, u
}

func TestCompletedOpeningExportWorkbook(t *testing.T) {
	s, admin, owner := completedTestSetup(t)
	a, first := completedTestSubmit(t, s, admin, owner, "first@example.test")
	require.NoError(t, s.Review(t.Context(), admin, first.ID, ReviewInput{Version: first.Version, Status: StatusRejected, Reason: "redo"}, AccountTypeOpening))
	v, err := s.GetAccount(t.Context(), owner, a.ID, AccountTypeOpening)
	require.NoError(t, err)
	latest, err := s.SubmitWithRemark(t.Context(), owner, v.AssignmentID, v.AssignmentVersion, nil, "final remark", AccountTypeOpening)
	require.NoError(t, err)
	require.NoError(t, s.Review(t.Context(), admin, latest.ID, ReviewInput{Version: latest.Version, Status: StatusApproved, Reason: "checked"}, AccountTypeOpening))
	// Update credentials without changing the current assignment or submission.
	_, err = s.WithImportUpdateExisting(true).Import(t.Context(), admin, "text", []byte("first@example.test----  =secret  ----XXXX----4242424242424242----12/99"), AccountTypeOpening)
	require.NoError(t, err)
	completedTestSubmit(t, s, admin, owner, "second@example.test")
	for _, status := range []string{StatusPending, StatusRejected, StatusIssuePending, "revoked", "archived"} {
		account, sub := completedTestSubmit(t, s, admin, owner, status+"@example.test")
		switch status {
		case "archived":
			require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", account.ID).Update("archived_at", 1).Error)
		case "revoked":
			require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", sub.AssignmentID).Update("revoked_at", 1).Error)
		default:
			require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", sub.AssignmentID).Update("status", status).Error)
		}
	}
	data, count, err := s.ExportCompletedOpening(t.Context(), admin)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Len(t, rows[0], 9)
	require.Equal(t, []string{"first@example.test", "  =secret  ", "XXXX", "Test", "已通过", "final remark", "2026-09-17 01:02:03", "2026-09-17 01:02:03", "checked"}, rows[1])
	require.Equal(t, "=1+1\n完成", rows[2][5])
	for _, cell := range []string{"B2", "F3"} {
		formula, err := f.GetCellFormula("Sheet1", cell)
		require.NoError(t, err)
		require.Empty(t, formula)
		kind, err := f.GetCellType("Sheet1", cell)
		require.NoError(t, err)
		require.Equal(t, excelize.CellTypeSharedString, kind)
	}
	panes, err := f.GetPanes("Sheet1")
	require.NoError(t, err)
	require.True(t, panes.Freeze)
	styleID, err := f.GetCellStyle("Sheet1", "F3")
	require.NoError(t, err)
	style, err := f.GetStyle(styleID)
	require.NoError(t, err)
	require.True(t, style.Alignment.WrapText)
}

func TestCompletedOpeningExportOTPAndPagination(t *testing.T) {
	s, admin, owner := completedTestSetup(t)
	_, err := s.Import(t.Context(), admin, "text", []byte("custom@example.test----pass----otpauth://totp/test?secret="+mailboxImportTestSecret+"&algorithm=SHA256&digits=8&period=60----4242424242424242----12/99"), AccountTypeOpening)
	require.NoError(t, err)
	var a model.MailboxAccount
	require.NoError(t, s.DB.Where("email = ?", "custom@example.test").First(&a).Error)
	v := cvvTestAssign(t, s, admin, owner, a.ID)
	_, err = s.SubmitWithRemark(t.Context(), owner, v.AssignmentID, v.AssignmentVersion, nil, "custom", AccountTypeOpening)
	require.NoError(t, err)
	// Populate more than one export batch with individually encrypted credentials.
	for i := 0; i < 251; i++ {
		completedTestSubmit(t, s, admin, owner, fmt.Sprintf("page%d@example.test", i))
	}
	data, count, err := s.ExportCompletedOpening(t.Context(), admin)
	require.NoError(t, err)
	require.Equal(t, 252, count)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows("Sheet1")
	require.NoError(t, err)
	require.Len(t, rows, 253)
	require.Len(t, rows[0], 12)
	require.Equal(t, []string{"SHA256", "8", "60"}, rows[1][9:])
}

func TestCompletedOpeningExportEmptyPermissionAndChanges(t *testing.T) {
	s, admin, owner := completedTestSetup(t)
	_, _, err := s.ExportCompletedOpening(t.Context(), owner)
	workflowTestStatus(t, err, 403)
	_, _, err = s.ExportCompletedOpening(t.Context(), admin)
	require.EqualError(t, err, "mailbox_export_completed_empty")
	a, _ := completedTestSubmit(t, s, admin, owner, "change@example.test")
	original := s.Cipher
	s.Cipher = func() (*managedinstance.CredentialCipher, error) {
		require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", a.ID).Update("archived_at", 1).Error)
		return original()
	}
	data, _, err := s.ExportCompletedOpening(t.Context(), admin)
	require.Error(t, err)
	require.Nil(t, data)
	s.Cipher = original
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", a.ID).Update("archived_at", 0).Error)
	s.Cipher = func() (*managedinstance.CredentialCipher, error) {
		require.NoError(t, s.DB.Model(&model.User{}).Where("id = ?", admin.Admin.UserID).Update("status", 2).Error)
		return original()
	}
	data, _, err = s.ExportCompletedOpening(t.Context(), admin)
	require.Error(t, err)
	require.Nil(t, data)
}

func TestCompletedOpeningExportLimit(t *testing.T) {
	s, admin, owner := completedTestSetup(t)
	accounts := make([]model.MailboxAccount, completedExportLimit+1)
	assignments := make([]model.MailboxAssignment, len(accounts))
	submissions := make([]model.MailboxSubmission, len(accounts))
	for i := range accounts {
		id := int64(i + 1)
		accounts[i] = model.MailboxAccount{ID: id, AccountType: AccountTypeOpening, Email: fmt.Sprintf("limit%d@example.test", i), ActiveAssignmentID: id, Version: 1}
		assignments[i] = model.MailboxAssignment{ID: id, AccountID: id, OperatorID: owner.OperatorID, Status: StatusSubmitted, Version: 1}
		submissions[i] = model.MailboxSubmission{ID: id, AssignmentID: id, OperatorID: owner.OperatorID, Status: StatusPending, Version: 1}
	}
	require.NoError(t, s.DB.CreateInBatches(&accounts, 100).Error)
	require.NoError(t, s.DB.CreateInBatches(&assignments, 100).Error)
	require.NoError(t, s.DB.CreateInBatches(&submissions, 100).Error)
	data, _, err := s.ExportCompletedOpening(t.Context(), admin)
	require.EqualError(t, err, "mailbox_export_completed_limit")
	require.Nil(t, data)
}

func TestCompletedOpeningExportRequiresAllAdminGrants(t *testing.T) {
	s, _, _ := completedTestSetup(t)
	require.NoError(t, s.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(s.DB))
	u := model.User{Username: "export-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, s.DB.Create(&u).Error)
	t.Cleanup(func() { _ = authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: {}}) })
	access, err := authz.LoadDataAccess(s.DB, u.Id)
	require.NoError(t, err)
	for _, missing := range []string{"view", "review", "credentials"} {
		grants := map[string]bool{"view": true, "review": true, "credentials": true}
		grants[missing] = false
		require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: grants}))
		_, _, err := s.ExportCompletedOpening(t.Context(), Actor{Admin: access})
		workflowTestStatus(t, err, 403)
		_, _, err = s.ExportAllData(t.Context(), Actor{Admin: access})
		workflowTestStatus(t, err, 403)
		_, _, err = s.ExportIssues(t.Context(), Actor{Admin: access}, IssueExportInput{Scope: "all"})
		workflowTestStatus(t, err, 403)
	}
}
