package mailbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func workTestTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return parsed
}

func workTestAssignment(t *testing.T, s *Service, operatorID, accountID, at int64, status string, active bool) model.MailboxAssignment {
	t.Helper()
	assignment := model.MailboxAssignment{OperatorID: operatorID, AccountID: accountID, Status: status, Version: 3, AssignedBy: 1, CreatedAt: at, UpdatedAt: at}
	if !active {
		assignment.RevokedAt = at + 1
	}
	require.NoError(t, s.DB.Create(&assignment).Error)
	if active {
		require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", accountID).Update("active_assignment_id", assignment.ID).Error)
	}
	return assignment
}

func workTestSubmit(t *testing.T, s *Service, assignment model.MailboxAssignment, times ...int64) {
	t.Helper()
	for _, at := range times {
		require.NoError(t, s.DB.Create(&model.MailboxSubmission{AssignmentID: assignment.ID, OperatorID: assignment.OperatorID, Status: StatusPending, CreatedAt: at}).Error)
	}
}

func workTestIssue(t *testing.T, s *Service, assignment model.MailboxAssignment, at int64, status string) {
	t.Helper()
	require.NoError(t, s.DB.Create(&model.MailboxIssue{AccountID: assignment.AccountID, AssignmentID: assignment.ID, OperatorID: assignment.OperatorID, Kind: "other", Description: "Synthetic issue", Status: status, CreatedAt: at}).Error)
}

func TestMailboxOperatorWorkRanges(t *testing.T) {
	s := New(nil)
	s.Now = func() time.Time { return workTestTime(t, "2026-09-15T17:23:45Z") }
	for _, tc := range []struct{ period, start, end string }{
		{"today", "2026-09-16T00:00:00+08:00", "2026-09-17T00:00:00+08:00"},
		{"last_7_days", "2026-09-10T00:00:00+08:00", "2026-09-17T00:00:00+08:00"},
		{"last_30_days", "2026-08-18T00:00:00+08:00", "2026-09-17T00:00:00+08:00"},
	} {
		t.Run(tc.period, func(t *testing.T) {
			_, r, err := s.normalizeWorkQuery(OperatorWorkQuery{Period: tc.period})
			require.NoError(t, err)
			require.Equal(t, WorkRange{Period: tc.period, StartAt: workTestTime(t, tc.start).Unix(), EndAt: workTestTime(t, tc.end).Unix(), Timezone: "Asia/Shanghai"}, r)
		})
	}
	q, r, err := s.normalizeWorkQuery(OperatorWorkQuery{})
	require.NoError(t, err)
	require.Equal(t, WorkRange{Period: "all", Timezone: "Asia/Shanghai"}, r)
	require.Equal(t, "all", q.AccountType)
	require.Equal(t, "submitted", q.Scope)
	require.Equal(t, 20, q.PageSize)
	_, r, err = s.normalizeWorkQuery(OperatorWorkQuery{Period: "custom", StartDate: "2024-02-29", EndDate: "2024-02-29"})
	require.NoError(t, err)
	require.EqualValues(t, 86400, r.EndAt-r.StartAt)
	require.Equal(t, workTestTime(t, "2024-03-01T00:00:00+08:00").Unix(), r.EndAt)
	for _, q := range []OperatorWorkQuery{
		{Period: "yesterday"}, {Period: "TODAY"}, {AccountType: "unknown"}, {Scope: "all"},
		{StartDate: "2026-09-16"}, {Period: "today", EndDate: "2026-09-16"},
		{Period: "custom"}, {Period: "custom", StartDate: "2026-09-16"},
		{Period: "custom", StartDate: "2026-02-29", EndDate: "2026-03-01"},
		{Period: "custom", StartDate: "2026-09-17", EndDate: "2026-09-16"},
		{Period: "custom", StartDate: "2026-9-16", EndDate: "2026-09-16"},
		{Period: "custom", StartDate: "0000-01-01", EndDate: "0000-01-01"},
	} {
		_, _, err := s.normalizeWorkQuery(q)
		operatorTestError(t, err, 400, "mailbox_invalid_work_query")
	}
}

func TestMailboxOperatorWorkDistinctRangesPoolsAndLifetime(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := context.Background()
	s.Now = func() time.Time { return workTestTime(t, "2026-09-16T12:00:00+08:00") }
	s.Cipher = func() (*managedinstance.CredentialCipher, error) {
		t.Fatal("statistics must never access credentials")
		return nil, nil
	}
	start := workTestTime(t, "2026-09-16T00:00:00+08:00").Unix()
	end := start + 86400
	refund := workflowTestAccount(t, s, "shared@example.test")
	opening := model.MailboxAccount{AccountType: AccountTypeOpening, Email: refund.Email, CardLast4: "4242", Ciphertext: "secret-mail", KeyVersion: "secret-key", CardCiphertext: "secret-card"}
	require.NoError(t, s.DB.Create(&opening).Error)
	old := workTestAssignment(t, s, owner.OperatorID, refund.ID, start-100, StatusRejected, false)
	latest := workTestAssignment(t, s, owner.OperatorID, refund.ID, start+1, StatusSubmitted, false)
	newOwner := workTestAssignment(t, s, other.OperatorID, refund.ID, start+2, StatusApproved, true)
	openTask := workTestAssignment(t, s, owner.OperatorID, opening.ID, start-100, StatusSubmitted, true)
	workTestSubmit(t, s, old, start-1, start)
	workTestSubmit(t, s, latest, start+1, end-1, end)
	var latestSubmission model.MailboxSubmission
	require.NoError(t, s.DB.Where("assignment_id = ?", latest.ID).Order("id DESC").First(&latestSubmission).Error)
	workTestSubmit(t, s, newOwner, start+2)
	workTestSubmit(t, s, openTask, start)
	workTestIssue(t, s, old, start-1, "invalidated")
	workTestIssue(t, s, latest, start, "resolved")
	workTestIssue(t, s, latest, end-1, "invalidated")
	workTestIssue(t, s, openTask, end, "resolved")
	// Archived accounts remain in historical event totals.
	require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", opening.ID).Update("archived_at", end).Error)
	overview, err := s.OperatorWorkSummary(ctx, admin, owner.OperatorID, OperatorWorkQuery{Period: "today"})
	require.NoError(t, err)
	require.Equal(t, WorkSummary{SubmittedAccounts: 2, RefundSubmitted: 1, OpeningSubmitted: 1, SubmissionCount: 4, IssueAccounts: 1}, overview.Summary)
	all, err := s.OperatorWorkSummary(ctx, admin, owner.OperatorID, OperatorWorkQuery{})
	require.NoError(t, err)
	require.EqualValues(t, 6, all.Summary.SubmissionCount)
	require.EqualValues(t, 2, all.Summary.IssueAccounts)
	page, err := s.OperatorWorkAccounts(ctx, admin, owner.OperatorID, OperatorWorkQuery{Period: "today", Search: "SHARED@", PageSize: 1})
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.True(t, page.HasMore)
	require.Equal(t, opening.ID, page.Items[0].ID)
	require.False(t, page.Items[0].AssignmentActive)
	require.Equal(t, end, page.Items[0].LastIssueAt)
	page, err = s.OperatorWorkAccounts(ctx, admin, owner.OperatorID, OperatorWorkQuery{Period: "today", Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.False(t, page.HasMore)
	require.Equal(t, OperatorWorkAccount{ID: refund.ID, Email: refund.Email, AccountType: "refund", AssignmentID: latest.ID, AssignmentVersion: 3, Status: StatusSubmitted, AssignedAt: start + 1, RevokedAt: start + 2, SubmissionCount: 5, LatestSubmissionID: latestSubmission.ID, LatestSubmissionStatus: StatusPending, LastSubmittedAt: end, LastIssueAt: end - 1}, page.Items[0])
	for scope, count := range map[string]int64{"assigned": 1, "submitted": 2, "issues": 1, "current_submitted": 0} {
		page, err := s.OperatorWorkAccounts(ctx, admin, owner.OperatorID, OperatorWorkQuery{Period: "today", Scope: scope})
		require.NoError(t, err)
		require.Equal(t, count, page.Total, scope)
	}
	page, err = s.OperatorWorkAccounts(ctx, admin, owner.OperatorID, OperatorWorkQuery{Period: "today", AccountType: "opening"})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, opening.ID, page.Items[0].ID)
	operators, err := s.ListOperatorsWithStats(ctx, admin, ListQuery{Search: "workflow-operator-0"}, OperatorWorkQuery{Period: "today", Search: "no-account-matches"})
	require.NoError(t, err)
	require.Len(t, operators.Items, 1)
	require.Equal(t, overview.Summary, *operators.Items[0].WorkSummary)
	ordinary, err := s.ListOperators(ctx, admin, ListQuery{})
	require.NoError(t, err)
	data, err := json.Marshal(ordinary)
	require.NoError(t, err)
	require.NotContains(t, string(data), "work_summary")
	for _, value := range []any{overview, page, operators} {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		for _, secret := range []string{"ciphertext", "password", "auth_version", "key_version", "secret-", "synthetic-", "card_number", "storage_key"} {
			require.NotContains(t, string(data), secret)
		}
	}
	zero, err := s.OperatorWorkSummary(ctx, admin, owner.OperatorID, OperatorWorkQuery{Period: "custom", StartDate: "2020-01-01", EndDate: "2020-01-01"})
	require.NoError(t, err)
	require.Equal(t, WorkSummary{}, zero.Summary)
}

func TestMailboxOperatorWorkCurrentConsistency(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	ctx := context.Background()
	for index, status := range []string{StatusPending, StatusSubmitted, StatusApproved, StatusRejected, StatusIssuePending} {
		a := workflowTestAccount(t, s, fmt.Sprintf("current-%d@example.test", index))
		task := workTestAssignment(t, s, owner.OperatorID, a.ID, s.Now().Unix(), status, true)
		if status == StatusIssuePending {
			workTestIssue(t, s, task, s.Now().Unix(), "pending")
			workTestIssue(t, s, task, s.Now().Unix(), "pending") // EXISTS, not a fan-out join.
		}
	}
	for index, mode := range []string{"no_issue", "resolved", "archived", "revoked", "inactive"} {
		a := workflowTestAccount(t, s, fmt.Sprintf("excluded-%d@example.test", index))
		task := workTestAssignment(t, s, owner.OperatorID, a.ID, s.Now().Unix(), StatusIssuePending, mode != "revoked")
		status := "pending"
		if mode == "resolved" {
			status = "resolved"
		}
		if mode != "no_issue" {
			workTestIssue(t, s, task, s.Now().Unix(), status)
		}
		if mode == "archived" {
			require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", a.ID).Update("archived_at", s.Now().Unix()).Error)
		}
		if mode == "inactive" {
			require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", a.ID).Update("active_assignment_id", 0).Error)
		}
	}
	for _, disabled := range []bool{false, true} {
		require.NoError(t, s.DB.Model(&model.MailboxOperator{}).Where("id = ?", owner.OperatorID).Update("enabled", !disabled).Error)
		query := OperatorWorkQuery{Period: "custom", StartDate: "2020-01-01", EndDate: "2020-01-01"}
		overview, err := s.OperatorWorkSummary(ctx, admin, owner.OperatorID, query)
		require.NoError(t, err)
		expectedIssue := int64(1)
		if disabled {
			expectedIssue = 0
		}
		require.Equal(t, WorkCurrent{Pending: 1, Submitted: 1, Approved: 1, Rejected: 1, IssuePending: expectedIssue}, overview.Summary.Current)
		for scope, count := range map[string]int64{"current_pending": 1, "current_submitted": 1, "current_approved": 1, "current_rejected": 1, "current_issue_pending": expectedIssue} {
			query.Scope = scope
			page, err := s.OperatorWorkAccounts(ctx, admin, owner.OperatorID, query)
			require.NoError(t, err)
			require.Equal(t, count, page.Total, scope)
			for _, a := range page.Items {
				require.True(t, a.AssignmentActive)
				require.Zero(t, a.SubmissionCount)
				require.Zero(t, a.LatestSubmissionID)
				require.Empty(t, a.LatestSubmissionStatus)
				require.Zero(t, a.LastSubmittedAt)
			}
		}
	}
}

func TestMailboxOperatorWorkRestoredAssignmentAndLatestSubmission(t *testing.T) {
	for _, mode := range []string{"restored", "inactive", "other_operator", "revoked", "archived"} {
		t.Run(mode, func(t *testing.T) {
			s, admin, owner, other := workflowTestService(t)
			ctx := context.Background()
			at := s.Now().Unix()
			a := workflowTestAccount(t, s, "restored@example.test")
			old := workTestAssignment(t, s, owner.OperatorID, a.ID, at-100, StatusApproved, false)
			newest := workTestAssignment(t, s, owner.OperatorID, a.ID, at-50, StatusPending, false)
			workTestSubmit(t, s, newest, at-40)
			// A newer submission can belong to an older assignment, with a result
			// distinct from both the active and newest historical assignment.
			latest := model.MailboxSubmission{AssignmentID: old.ID, OperatorID: owner.OperatorID, Status: StatusRejected, CreatedAt: at - 40}
			require.NoError(t, s.DB.Create(&latest).Error)
			foreign := workTestAssignment(t, s, other.OperatorID, a.ID, at-20, StatusSubmitted, false)
			workTestSubmit(t, s, foreign, at-10)
			otherAccount := workflowTestAccount(t, s, "unrelated@example.test")
			unrelated := workTestAssignment(t, s, owner.OperatorID, otherAccount.ID, at-20, StatusSubmitted, false)
			workTestSubmit(t, s, unrelated, at-10)
			workTestIssue(t, s, old, at-30, "resolved")
			workTestIssue(t, s, newest, at-20, "invalidated")
			require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", old.ID).Update("revoked_at", 0).Error)
			activeID := old.ID
			if mode == "inactive" {
				activeID = 0
			} else if mode == "other_operator" {
				activeID = foreign.ID
				require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", foreign.ID).Update("revoked_at", 0).Error)
			} else if mode == "revoked" {
				require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", old.ID).Update("revoked_at", at).Error)
			} else if mode == "archived" {
				require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", a.ID).Update("archived_at", at).Error)
			}
			require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", a.ID).Update("active_assignment_id", activeID).Error)
			expected := newest
			if mode == "restored" {
				expected = old
				expected.RevokedAt = 0
			}
			history, err := s.OperatorAccountHistory(ctx, admin, owner.OperatorID, a.ID, OperatorHistoryQuery{AccountType: "refund", PageSize: 1})
			require.NoError(t, err)
			require.Equal(t, expected.ID, history.Account.AssignmentID)
			require.Equal(t, expected.Status, history.Account.Status)
			require.Equal(t, expected.Version, history.Account.AssignmentVersion)
			require.Equal(t, expected.CreatedAt, history.Account.AssignedAt)
			require.Equal(t, expected.RevokedAt, history.Account.RevokedAt)
			require.Equal(t, mode == "restored", history.Account.AssignmentActive)
			require.Equal(t, latest.ID, history.Account.LatestSubmissionID)
			require.Equal(t, StatusRejected, history.Account.LatestSubmissionStatus)
			require.EqualValues(t, 2, history.Account.SubmissionCount)
			require.Equal(t, at-40, history.Account.LastSubmittedAt)
			require.Equal(t, at-20, history.Account.LastIssueAt)
			require.EqualValues(t, 2, history.Assignments.Total)
			require.EqualValues(t, 2, history.Submissions.Total)
			require.EqualValues(t, 2, history.Issues.Total)
			require.Equal(t, newest.ID, history.Assignments.Items[0].ID)
			require.Equal(t, latest.ID, history.Submissions.Items[0].ID)
			data, err := json.Marshal(history.Account)
			require.NoError(t, err)
			require.Contains(t, string(data), fmt.Sprintf(`"latest_submission_id":%d`, latest.ID))
			require.Contains(t, string(data), `"latest_submission_status":"rejected"`)
			for _, scope := range []string{"assigned", "submitted", "issues", "current_approved", "current_pending"} {
				page, err := s.OperatorWorkAccounts(ctx, admin, owner.OperatorID, OperatorWorkQuery{Scope: scope, Search: a.Email, PageSize: 1})
				require.NoError(t, err)
				if scope == "current_pending" || (scope == "current_approved" && mode != "restored") {
					require.Zero(t, page.Total, scope)
					require.Empty(t, page.Items)
					continue
				}
				require.EqualValues(t, 1, page.Total, scope)
				require.False(t, page.HasMore)
				require.Equal(t, []OperatorWorkAccount{history.Account}, page.Items)
			}
			overview, err := s.OperatorWorkSummary(ctx, admin, owner.OperatorID, OperatorWorkQuery{})
			require.NoError(t, err)
			expectedSummary := WorkSummary{SubmittedAccounts: 2, RefundSubmitted: 2, SubmissionCount: 3, IssueAccounts: 1}
			if mode == "restored" {
				expectedSummary.Current.Approved = 1
			}
			require.Equal(t, expectedSummary, overview.Summary)
		})
	}
}

func TestMailboxOperatorWorkTrueHistoryPaginationAndIsolation(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := context.Background()
	a := workflowTestAccount(t, s, "history@example.test")
	firstAssignment := workflowTestAssign(t, s, admin, owner, a.ID)
	first := workflowTestSubmission(t, s, owner, a.ID)
	require.NoError(t, s.Review(ctx, admin, first.ID, ReviewInput{Version: 1, Status: StatusRejected, Reason: "Try again"}))
	current, err := s.GetAccount(ctx, owner, a.ID)
	require.NoError(t, err)
	file := workflowTestUpload(t, s, owner, current.AssignmentID)
	issue, err := s.SubmitIssue(ctx, owner, current.AssignmentID, IssueInput{Version: current.AssignmentVersion, Kind: "other", Description: "First issue", AttachmentIDs: []string{file.ID}}, "refund")
	require.NoError(t, err)
	require.NoError(t, s.ResolveIssue(ctx, admin, issue.ID, issueResolution(issue), "refund"))
	second := workflowTestSubmission(t, s, owner, a.ID)
	legacyTestReassign(t, s, admin, other, a.ID)
	otherSubmission := workflowTestSubmission(t, s, other, a.ID)
	latest := legacyTestReassign(t, s, admin, owner, a.ID)
	issue2, err := s.SubmitIssue(ctx, owner, latest.AssignmentID, IssueInput{Version: latest.AssignmentVersion, Kind: "other", Description: "Second issue"}, "refund")
	require.NoError(t, err)
	current, err = s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	require.NoError(t, s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{ID: a.ID, Version: current.Version}}, OperatorID: 0}))
	current, err = s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	require.NoError(t, s.ArchiveAccounts(ctx, admin, ArchiveInput{AccountType: "refund", Items: []AssignItem{{ID: a.ID, Version: current.Version}}}, false))
	var op model.MailboxOperator
	require.NoError(t, s.DB.First(&op, owner.OperatorID).Error)
	_, err = s.SaveOperator(ctx, admin, op.ID, OperatorInput{Username: op.Username, DisplayName: op.DisplayName, Enabled: false, Version: op.Version})
	require.NoError(t, err)
	history, err := s.OperatorAccountHistory(ctx, admin, owner.OperatorID, a.ID, OperatorHistoryQuery{AccountType: "refund", AssignmentPage: 2, SubmissionPage: 1, IssuePage: 2, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, latest.AssignmentID, history.Account.AssignmentID)
	require.False(t, history.Account.AssignmentActive)
	require.NotZero(t, history.Account.ArchivedAt)
	require.EqualValues(t, 2, history.Account.SubmissionCount)
	require.Equal(t, second.ID, history.Account.LatestSubmissionID)
	require.Equal(t, StatusPending, history.Account.LatestSubmissionStatus)
	require.EqualValues(t, 2, history.Assignments.Total)
	require.EqualValues(t, 2, history.Submissions.Total)
	require.EqualValues(t, 2, history.Issues.Total)
	require.Equal(t, firstAssignment.AssignmentID, history.Assignments.Items[0].ID)
	require.Equal(t, second.ID, history.Submissions.Items[0].ID)
	require.Equal(t, issue.ID, history.Issues.Items[0].ID)
	require.False(t, history.Assignments.HasMore)
	require.True(t, history.Submissions.HasMore)
	require.False(t, history.Issues.HasMore)
	require.Len(t, history.Submissions.Items[0].Attachments, 1)
	require.Len(t, history.Issues.Items[0].Attachments, 1)
	require.False(t, history.Issues.Items[0].AssignmentActive)
	history, err = s.OperatorAccountHistory(ctx, admin, owner.OperatorID, a.ID, OperatorHistoryQuery{AccountType: "refund", SubmissionPage: 2, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, first.ID, history.Submissions.Items[0].ID)
	require.Equal(t, issue2.ID, history.Issues.Items[0].ID)
	require.Equal(t, "invalidated", history.Issues.Items[0].Status)
	history, err = s.OperatorAccountHistory(ctx, admin, other.OperatorID, a.ID, OperatorHistoryQuery{AccountType: "refund"})
	require.NoError(t, err)
	require.EqualValues(t, 1, history.Submissions.Total)
	require.Equal(t, otherSubmission.ID, history.Submissions.Items[0].ID)
	require.Empty(t, history.Issues.Items)
	data, err := json.Marshal(history)
	require.NoError(t, err)
	for _, secret := range []string{"password", "ciphertext", "auth_version", "storage_key", "synthetic-", "credentials", "card_number"} {
		require.NotContains(t, string(data), secret)
	}
	_, err = s.OperatorAccountHistory(ctx, admin, owner.OperatorID, a.ID, OperatorHistoryQuery{AccountType: "opening"})
	operatorTestError(t, err, 404, "mailbox_not_found")
	for _, kind := range []string{"", "all", "invalid"} {
		_, err = s.OperatorAccountHistory(ctx, admin, owner.OperatorID, a.ID, OperatorHistoryQuery{AccountType: kind})
		operatorTestError(t, err, 400, "mailbox_invalid_work_query")
	}
	untouched := workflowTestAccount(t, s, "untouched@example.test")
	_, err = s.OperatorAccountHistory(ctx, admin, owner.OperatorID, untouched.ID, OperatorHistoryQuery{AccountType: "refund"})
	operatorTestError(t, err, 404, "mailbox_not_found")
	history, err = s.OperatorAccountHistory(ctx, admin, owner.OperatorID, a.ID, OperatorHistoryQuery{AccountType: "refund", AssignmentPage: 99, SubmissionPage: 99, IssuePage: 99})
	require.NoError(t, err)
	require.Empty(t, history.Assignments.Items)
	require.Empty(t, history.Submissions.Items)
	require.Empty(t, history.Issues.Items)
	require.Equal(t, 20, history.Issues.PageSize)
}

type workTestLogger struct {
	logger.Interface
	queries int
	after   func(string)
}

func (l *workTestLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	l.queries++
	if l.after != nil {
		sql, _ := fc()
		l.after(sql)
	}
}

func workTestCalls(s *Service, actor Actor, operatorID, accountID int64) map[string]func() error {
	ctx := context.Background()
	return map[string]func() error{
		"list": func() error {
			_, err := s.ListOperatorsWithStats(ctx, actor, ListQuery{}, OperatorWorkQuery{})
			return err
		},
		"summary": func() error { _, err := s.OperatorWorkSummary(ctx, actor, operatorID, OperatorWorkQuery{}); return err },
		"accounts": func() error {
			_, err := s.OperatorWorkAccounts(ctx, actor, operatorID, OperatorWorkQuery{Scope: "assigned"})
			return err
		},
		"history": func() error {
			_, err := s.OperatorAccountHistory(ctx, actor, operatorID, accountID, OperatorHistoryQuery{AccountType: "refund"})
			return err
		},
	}
}

func TestMailboxOperatorWorkPermissionsAndRechecks(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	a := workflowTestAccount(t, s, "permissions@example.test")
	task := workTestAssignment(t, s, owner.OperatorID, a.ID, s.Now().Unix(), StatusPending, true)
	workTestIssue(t, s, task, s.Now().Unix(), "resolved")
	for _, actor := range []Actor{owner, {}} {
		for _, call := range workTestCalls(s, actor, owner.OperatorID, a.ID) {
			operatorTestError(t, call(), 403, "mailbox_permission_denied")
		}
	}
	master := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = master })
	require.NoError(t, s.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(s.DB))
	u := model.User{Username: "work-stat-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, s.DB.Create(&u).Error)
	t.Cleanup(func() {
		require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: {}}))
	})
	access, err := authz.LoadDataAccess(s.DB, u.Id)
	require.NoError(t, err)
	actor := Actor{Admin: access}
	grant := func(denied string) {
		permissions := map[string]bool{"operators": true, "view": true, "review": true}
		if denied != "" {
			permissions[denied] = false
		}
		require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: permissions}))
	}
	for _, denied := range []string{"operators", "view", "review"} {
		grant(denied)
		for name, call := range workTestCalls(s, actor, owner.OperatorID, a.ID) {
			t.Run(denied+"/"+name, func(t *testing.T) { operatorTestError(t, call(), 403, "mailbox_permission_denied") })
		}
	}
	grant("")
	for _, call := range workTestCalls(s, actor, owner.OperatorID, a.ID) {
		require.NoError(t, call())
	}
	for _, denied := range []string{"operators", "view", "review"} {
		for _, method := range []string{"list", "summary", "accounts", "history"} {
			t.Run("recheck/"+denied+"/"+method, func(t *testing.T) {
				grant("")
				triggered := false
				log := &workTestLogger{Interface: logger.Default.LogMode(logger.Silent)}
				log.after = func(sql string) {
					match := strings.Contains(sql, "SUM(CASE")
					if method == "accounts" {
						match = strings.HasPrefix(sql, "SELECT a.id, a.email")
					}
					if method == "history" {
						match = strings.HasPrefix(sql, "SELECT i.submitted_version")
					}
					if !triggered && match {
						triggered = true
						grant(denied)
					}
				}
				local := s.WithDB(s.DB.Session(&gorm.Session{Logger: log}))
				operatorTestError(t, workTestCalls(local, actor, owner.OperatorID, a.ID)[method](), 403, "mailbox_permission_denied")
				require.True(t, triggered)
			})
		}
	}
	require.NoError(t, s.DB.Model(&model.User{}).Where("id = ?", admin.Admin.UserID).UpdateColumn("authorization_version", admin.Admin.Version+1).Error)
	for _, call := range workTestCalls(s, admin, owner.OperatorID, a.ID) {
		require.ErrorIs(t, call(), authz.ErrAuthorizationChanged)
	}
}

func TestMailboxOperatorWorkBatchedQueriesAndEmptyResults(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := context.Background()
	for _, id := range []int64{owner.OperatorID, other.OperatorID} {
		a := workflowTestAccount(t, s, fmt.Sprintf("batched-%d@example.test", id))
		task := workTestAssignment(t, s, id, a.ID, s.Now().Unix(), StatusSubmitted, true)
		workTestSubmit(t, s, task, s.Now().Unix())
	}
	log := &workTestLogger{Interface: logger.Default.LogMode(logger.Silent)}
	s = s.WithDB(s.DB.Session(&gorm.Session{Logger: log}))
	_, err := s.ListOperatorsWithStats(ctx, admin, ListQuery{PageSize: 1}, OperatorWorkQuery{})
	require.NoError(t, err)
	one := log.queries
	log.queries = 0
	page, err := s.ListOperatorsWithStats(ctx, admin, ListQuery{PageSize: 100}, OperatorWorkQuery{})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Equal(t, one, log.queries, "SQL query count must not grow with the operator page")
	page, err = s.ListOperatorsWithStats(ctx, admin, ListQuery{Search: "missing"}, OperatorWorkQuery{})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	_, err = s.OperatorWorkSummary(ctx, admin, -1, OperatorWorkQuery{})
	operatorTestError(t, err, 404, "mailbox_not_found")
	_, err = s.OperatorWorkAccounts(ctx, admin, 999, OperatorWorkQuery{})
	operatorTestError(t, err, 404, "mailbox_not_found")
	_, err = s.OperatorAccountHistory(ctx, admin, 999, 1, OperatorHistoryQuery{AccountType: "refund"})
	operatorTestError(t, err, 404, "mailbox_not_found")
}
