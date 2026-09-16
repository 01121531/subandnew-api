package mailbox

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func dataExportSetup(t *testing.T) (*Service, Actor, Actor) {
	s, admin, owner := completedTestSetup(t)
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxIssue{}, &model.MailboxRemarkRevision{}))
	return s, admin, owner
}

func dataExportRows(t *testing.T, data []byte, sheet string) [][]string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	return rows
}

func TestDataExportBothPoolsAndHistory(t *testing.T) {
	s, admin, owner := dataExportSetup(t)
	a, sub := completedTestSubmit(t, s, admin, owner, "same@example.test")
	_, err := s.EditRemark(t.Context(), admin, sub.ID, RemarkInput{AccountType: AccountTypeOpening, Version: sub.Version, Remark: "=1+1\n修订"})
	require.NoError(t, err)
	_, err = s.Import(t.Context(), admin, "text", []byte("same@example.test----  =secret  ----XXXX"), AccountTypeRefund)
	require.NoError(t, err)
	// Archived and revoked history remains present and is not mistaken for a current assignment.
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", a.ID).Updates(map[string]any{"archived_at": 1, "active_assignment_id": 0, "version": 5}).Error)
	require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", sub.AssignmentID).Update("revoked_at", 1).Error)
	issue := model.MailboxIssue{AccountID: a.ID, AssignmentID: sub.AssignmentID, OperatorID: owner.OperatorID, Kind: "card", Description: "=feedback", Status: "invalidated", CreatedAt: s.Now().Unix(), Version: 1}
	require.NoError(t, s.DB.Create(&issue).Error)
	data, counts, err := s.ExportAllData(t.Context(), admin)
	require.NoError(t, err)
	require.Equal(t, map[string]int{"邮箱资料": 2, "分配历史": 1, "提交审核": 1, "异常反馈": 1, "备注修订": 1}, counts)
	rows := dataExportRows(t, data, "邮箱资料")
	require.Len(t, rows, 3)
	require.Equal(t, "是", rows[1][11])
	require.Equal(t, "  =secret  ", rows[2][3])
	require.Equal(t, "XXXX", rows[2][4])
	require.Equal(t, "未分配", rows[1][8])
	require.Equal(t, "2026-09-17 01:02:03", rows[2][12])
	revisions := dataExportRows(t, data, "备注修订")
	require.Equal(t, fmt.Sprint(sub.ID), revisions[1][3])
	require.Equal(t, "=1+1\n修订", revisions[1][7])
	u := dataExportRows(t, data, "提交审核")
	require.Equal(t, "=1+1\n修订", u[1][8])
	require.Equal(t, "待审核", u[1][7])
	feedback := dataExportRows(t, data, "异常反馈")
	require.Equal(t, "否", feedback[1][16])
	require.Equal(t, "=feedback", feedback[1][8])
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	for _, sheet := range f.GetSheetList() {
		r, e := f.GetRows(sheet)
		require.NoError(t, e)
		require.NotContains(t, fmt.Sprint(r), "4242424242424242")
		require.NotContains(t, strings.Join(r[0], ","), "CVV")
		p, e := f.GetPanes(sheet)
		require.NoError(t, e)
		require.True(t, p.Freeze)
	}
	formula, err := f.GetCellFormula("邮箱资料", "D3")
	require.NoError(t, err)
	require.Empty(t, formula)
	kind, err := f.GetCellType("提交审核", "I2")
	require.NoError(t, err)
	require.Equal(t, excelize.CellTypeSharedString, kind)
}

func TestDataExportIssueFiltersAndDeduplication(t *testing.T) {
	s, admin, owner := dataExportSetup(t)
	a, sub := completedTestSubmit(t, s, admin, owner, "one@example.test")
	b, sub2 := completedTestSubmit(t, s, admin, owner, "two@example.test")
	for _, row := range []model.MailboxIssue{
		{AccountID: a.ID, AssignmentID: sub.AssignmentID, OperatorID: owner.OperatorID, Kind: "card", Status: "resolved", Description: "first", Version: 1},
		{AccountID: a.ID, AssignmentID: sub.AssignmentID, OperatorID: owner.OperatorID, Kind: "card", Status: "pending", Description: "second", Version: 1},
		{AccountID: b.ID, AssignmentID: sub2.AssignmentID, OperatorID: owner.OperatorID, Kind: "other", Status: "invalidated", Description: "third", Version: 1},
	} {
		require.NoError(t, s.DB.Create(&row).Error)
	}
	data, counts, err := s.ExportIssues(t.Context(), admin, IssueExportInput{Scope: "all"})
	require.NoError(t, err)
	require.Equal(t, 3, counts["异常反馈"])
	require.Equal(t, 2, counts["关联邮箱资料"])
	require.Len(t, dataExportRows(t, data, "异常反馈"), 4)
	input := IssueExportInput{Scope: "filtered", AccountType: AccountTypeOpening, Status: "processed", Search: "ONE@", Kind: "card", OperatorID: owner.OperatorID}
	_, counts, err = s.ExportIssues(t.Context(), admin, input)
	require.NoError(t, err)
	require.Equal(t, 1, counts["异常反馈"])
	page, err := s.ListIssues(t.Context(), admin, ListQuery{AccountType: AccountTypeOpening, Status: input.Status, Search: input.Search, Kind: input.Kind, OperatorID: owner.OperatorID})
	require.NoError(t, err)
	require.EqualValues(t, page.Total, counts["异常反馈"])
	_, _, err = s.ExportIssues(t.Context(), admin, IssueExportInput{Scope: "filtered", AccountType: AccountTypeRefund, Kind: "card"})
	require.EqualError(t, err, "mailbox_invalid_query")
	_, _, err = s.ExportIssues(t.Context(), admin, IssueExportInput{Scope: "all", Search: "x"})
	require.EqualError(t, err, "mailbox_invalid_query")
	_, _, err = s.ExportIssues(t.Context(), admin, IssueExportInput{Scope: "filtered", AccountType: AccountTypeRefund})
	require.EqualError(t, err, "mailbox_export_empty")
}

func TestDataExportPaginationOTPAndLimit(t *testing.T) {
	s, admin, _ := dataExportSetup(t)
	var lines []string
	for i := 0; i < 251; i++ {
		lines = append(lines, fmt.Sprintf("page%d@example.test----p----otpauth://totp/test?secret=%s&algorithm=SHA256&digits=8&period=60", i, mailboxImportTestSecret))
	}
	_, err := s.Import(t.Context(), admin, "text", []byte(strings.Join(lines, "\n")), AccountTypeRefund)
	require.NoError(t, err)
	data, counts, err := s.ExportAllData(t.Context(), admin)
	require.NoError(t, err)
	require.Equal(t, 251, counts["邮箱资料"])
	rows := dataExportRows(t, data, "邮箱资料")
	require.Len(t, rows, 252)
	require.Equal(t, []string{"SHA256", "8", "60"}, rows[1][5:8])
	accounts := make([]model.MailboxAccount, 10000)
	for i := range accounts {
		accounts[i] = model.MailboxAccount{AccountType: AccountTypeRefund, Email: fmt.Sprintf("limit%d@example.test", i), Version: 1}
	}
	require.NoError(t, s.DB.CreateInBatches(accounts, 100).Error)
	data, _, err = s.ExportAllData(t.Context(), admin)
	require.EqualError(t, err, "mailbox_export_limit")
	require.Nil(t, data)
}

func TestDataExportPermissionAndConcurrentChange(t *testing.T) {
	s, admin, owner := dataExportSetup(t)
	_, _, err := s.ExportAllData(t.Context(), owner)
	workflowTestStatus(t, err, 403)
	_, _, err = s.ExportIssues(t.Context(), owner, IssueExportInput{Scope: "all"})
	workflowTestStatus(t, err, 403)
	_, _, err = s.ExportAllData(t.Context(), admin)
	require.EqualError(t, err, "mailbox_export_empty")
	a, _ := completedTestSubmit(t, s, admin, owner, "changed@example.test")
	original := s.Cipher
	s.Cipher = func() (*managedinstance.CredentialCipher, error) {
		require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", a.ID).Update("version", 20).Error)
		return original()
	}
	data, _, err := s.ExportAllData(t.Context(), admin)
	require.EqualError(t, err, "mailbox_export_changed")
	require.Nil(t, data)
	s.Cipher = func() (*managedinstance.CredentialCipher, error) {
		require.NoError(t, s.DB.Model(&model.User{}).Where("id = ?", admin.Admin.UserID).Update("status", 2).Error)
		return original()
	}
	data, _, err = s.ExportAllData(t.Context(), admin)
	require.Error(t, err)
	require.Nil(t, data)
}

func TestDataExportPreservesReassignmentAndResubmission(t *testing.T) {
	s, admin, owner := dataExportSetup(t)
	a, first := completedTestSubmit(t, s, admin, owner, "history@example.test")
	require.NoError(t, s.Review(t.Context(), admin, first.ID, ReviewInput{Version: first.Version, Status: StatusRejected, Reason: "retry"}, AccountTypeOpening))
	view, err := s.GetAccount(t.Context(), owner, a.ID, AccountTypeOpening)
	require.NoError(t, err)
	second, err := s.SubmitWithRemark(t.Context(), owner, view.AssignmentID, view.AssignmentVersion, nil, "resubmitted", AccountTypeOpening)
	require.NoError(t, err)
	op := model.MailboxOperator{Username: "second", DisplayName: "Second", Enabled: false, Version: 1, AuthVersion: 1}
	require.NoError(t, s.DB.Create(&op).Error)
	assignment := model.MailboxAssignment{AccountID: a.ID, OperatorID: op.ID, Status: StatusApproved, Version: 1}
	require.NoError(t, s.DB.Create(&assignment).Error)
	third := model.MailboxSubmission{AssignmentID: assignment.ID, OperatorID: op.ID, Status: StatusApproved, Remark: "another operator", Version: 1}
	require.NoError(t, s.DB.Create(&third).Error)
	require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", second.AssignmentID).Update("revoked_at", s.Now().Unix()).Error)
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", a.ID).Update("active_assignment_id", assignment.ID).Error)
	data, counts, err := s.ExportAllData(t.Context(), admin)
	require.NoError(t, err)
	require.Equal(t, 1, counts["邮箱资料"])
	require.Equal(t, 2, counts["分配历史"])
	require.Equal(t, 3, counts["提交审核"])
	rows := dataExportRows(t, data, "提交审核")
	require.Equal(t, "已退回", rows[1][7])
	require.Equal(t, "resubmitted", rows[2][8])
	require.Equal(t, "Second", rows[3][6])
	require.Equal(t, "another operator", rows[3][8])
}
