package mailbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func workflowTestService(t *testing.T) (*Service, Actor, Actor, Actor) {
	t.Helper()
	dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "workflow.db")) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(8)
	require.NoError(t, db.AutoMigrate(&model.MailboxIssue{}))
	require.NoError(t, db.AutoMigrate(&model.MailboxCVV{}, &model.MailboxCardIndex{}, &model.MailboxCardIndexState{}))
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AdminDataPolicy{}, &model.MailboxAccount{}, &model.MailboxOperator{}, &model.MailboxSession{}, &model.MailboxAssignment{}, &model.MailboxSubmission{}, &model.MailboxAttachment{}, &model.MailboxAudit{}))
	user := model.User{Username: "workflow-test-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	admin, err := authz.LoadDataAccess(db, user.Id)
	require.NoError(t, err)
	s := New(db)
	s.Now = func() time.Time { return time.Unix(2000000000, 0) }
	s.StorageDir = filepath.Join(t.TempDir(), "private")
	operators := make([]Actor, 2)
	for i := range operators {
		op := model.MailboxOperator{Username: fmt.Sprintf("workflow-operator-%d", i), DisplayName: fmt.Sprintf("Operator %d", i), PasswordHash: "synthetic-unusable-hash", Enabled: true, AuthVersion: 1, Version: 1}
		require.NoError(t, db.Create(&op).Error)
		hash := digest(fmt.Sprintf("synthetic-workflow-session-%d", i))
		require.NoError(t, db.Create(&model.MailboxSession{TokenHash: hash, OperatorID: op.ID, AuthVersion: 1, ExpiresAt: s.Now().Unix() + 86400}).Error)
		operators[i] = Actor{OperatorID: op.ID, AuthVersion: 1, SessionHash: hash}
	}
	return s, Actor{Admin: admin}, operators[0], operators[1]
}

func workflowTestAccount(t *testing.T, s *Service, email string) model.MailboxAccount {
	t.Helper()
	a := model.MailboxAccount{Email: email, Ciphertext: "synthetic-ciphertext-never-return", KeyVersion: "synthetic-key-version", Version: 1}
	require.NoError(t, s.DB.Create(&a).Error)
	return a
}

func workflowTestAssign(t *testing.T, s *Service, admin, operator Actor, accountID int64) *AccountView {
	t.Helper()
	view, err := s.GetAccount(context.Background(), admin, accountID)
	require.NoError(t, err)
	require.NoError(t, s.Assign(context.Background(), admin, AssignInput{Items: []AssignItem{{ID: accountID, Version: view.Version}}, OperatorID: operator.OperatorID}))
	view, err = s.GetAccount(context.Background(), operator, accountID)
	require.NoError(t, err)
	return view
}

func workflowTestUpload(t *testing.T, s *Service, actor Actor, assignmentID int64) *AttachmentView {
	t.Helper()
	file, err := s.UploadAttachment(context.Background(), actor, assignmentID, bytes.NewReader(mailboxTestPNG(t)))
	require.NoError(t, err)
	return file
}

func workflowTestSubmission(t *testing.T, s *Service, actor Actor, accountID int64) *SubmissionView {
	t.Helper()
	view, err := s.GetAccount(context.Background(), actor, accountID)
	require.NoError(t, err)
	file := workflowTestUpload(t, s, actor, view.AssignmentID)
	submission, err := s.Submit(context.Background(), actor, view.AssignmentID, view.AssignmentVersion, []string{file.ID})
	require.NoError(t, err)
	return submission
}

func workflowTestStatus(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	actual, _ := HTTPError(err)
	require.Equal(t, status, actual)
}

func TestMailboxWorkflowLifecycleAndHistoryIsolation(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := context.Background()
	account := workflowTestAccount(t, s, "lifecycle@example.test")
	view := workflowTestAssign(t, s, admin, owner, account.ID)
	require.Equal(t, StatusPending, view.Status)
	require.True(t, view.CredentialsAvailable)
	_, err := s.GetAccount(ctx, other, account.ID)
	workflowTestStatus(t, err, 404)
	first := workflowTestSubmission(t, s, owner, account.ID)
	require.Equal(t, StatusPending, first.Status)
	require.Len(t, first.Attachments, 1)
	require.Equal(t, s.Now().Unix()+365*86400, first.Attachments[0].ExpiresAt)
	view, err = s.GetAccount(ctx, owner, account.ID)
	require.NoError(t, err)
	require.Equal(t, StatusSubmitted, view.Status)
	require.True(t, view.CredentialsAvailable)
	_, err = s.UploadAttachment(ctx, owner, view.AssignmentID, bytes.NewReader(mailboxTestPNG(t)))
	workflowTestStatus(t, err, 409)
	require.NoError(t, s.Review(ctx, admin, first.ID, ReviewInput{Version: 1, Status: StatusRejected, Reason: "Please show the complete panel."}))
	second := workflowTestSubmission(t, s, owner, account.ID)
	require.NotEqual(t, first.ID, second.ID)
	workflowTestStatus(t, s.Review(ctx, admin, first.ID, ReviewInput{Version: 2, Status: StatusApproved}), 409)
	require.NoError(t, s.Review(ctx, admin, second.ID, ReviewInput{Version: 1, Status: StatusApproved}))
	view, err = s.GetAccount(ctx, owner, account.ID)
	require.NoError(t, err)
	require.Equal(t, StatusApproved, view.Status)
	require.False(t, view.CredentialsAvailable)
	_, err = s.AccountForActor(owner, account.ID, true)
	workflowTestStatus(t, err, 403)
	history, err := s.ListSubmissions(ctx, owner, ListQuery{Search: "LIFECYCLE@"})
	require.NoError(t, err)
	require.EqualValues(t, 2, history.Total)
	require.Equal(t, "Please show the complete panel.", history.Items[1].ReviewReason)
	workflowTestAssign(t, s, admin, other, account.ID)
	_, err = s.GetAccount(ctx, owner, account.ID)
	workflowTestStatus(t, err, 404)
	_, _, err = s.ReadAttachment(ctx, other, first.Attachments[0].ID)
	workflowTestStatus(t, err, 404)
	_, _, err = s.ReadAttachment(ctx, owner, first.Attachments[0].ID)
	require.NoError(t, err)
	otherHistory, err := s.ListSubmissions(ctx, other, ListQuery{})
	require.NoError(t, err)
	require.Zero(t, otherHistory.Total)
	history, err = s.ListSubmissions(ctx, owner, ListQuery{})
	require.NoError(t, err)
	require.EqualValues(t, 2, history.Total)
	encoded, err := json.Marshal(history)
	require.NoError(t, err)
	for _, secret := range []string{"ciphertext", "storage_key", "password_hash", "synthetic-", s.StorageDir} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestMailboxWorkflowAssignAtomicRecallAndValidation(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := context.Background()
	a := workflowTestAccount(t, s, "a@example.test")
	b := workflowTestAccount(t, s, "b@example.test")
	workflowTestStatus(t, s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{a.ID, 1}, {b.ID, 99}}, OperatorID: owner.OperatorID}), 409)
	for _, account := range []model.MailboxAccount{a, b} {
		view, err := s.GetAccount(ctx, admin, account.ID)
		require.NoError(t, err)
		require.Equal(t, "unassigned", view.Status)
		require.EqualValues(t, 1, view.Version)
	}
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Count(&count).Error)
	require.Zero(t, count)
	for _, items := range [][]AssignItem{nil, {{a.ID, 1}, {a.ID, 1}}, {{a.ID, 0}}, {{0, 1}}, make([]AssignItem, 1001)} {
		workflowTestStatus(t, s.Assign(ctx, admin, AssignInput{Items: items, OperatorID: owner.OperatorID}), 400)
	}
	require.NoError(t, s.DB.Model(&model.MailboxOperator{}).Where("id = ?", other.OperatorID).Update("enabled", false).Error)
	workflowTestStatus(t, s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{a.ID, 1}}, OperatorID: other.OperatorID}), 404)
	require.NoError(t, s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{b.ID, 1}, {a.ID, 1}}, OperatorID: owner.OperatorID}))
	view, err := s.GetAccount(ctx, owner, a.ID)
	require.NoError(t, err)
	draft := workflowTestUpload(t, s, owner, view.AssignmentID)
	view, err = s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	require.NoError(t, s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{a.ID, view.Version}}, OperatorID: 0}))
	_, err = s.Submit(ctx, owner, view.AssignmentID, view.AssignmentVersion, []string{draft.ID})
	workflowTestStatus(t, err, 404)
	_, _, err = s.ReadAttachment(ctx, owner, draft.ID)
	workflowTestStatus(t, err, 404)
	var revoked model.MailboxAssignment
	require.NoError(t, s.DB.First(&revoked, view.AssignmentID).Error)
	require.NotZero(t, revoked.RevokedAt)
	view, err = s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	require.Equal(t, "unassigned", view.Status)
}

func TestMailboxWorkflowSubmissionValidationAndRollback(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := context.Background()
	a := workflowTestAccount(t, s, "submission@example.test")
	b := workflowTestAccount(t, s, "other@example.test")
	view := workflowTestAssign(t, s, admin, owner, a.ID)
	otherView := workflowTestAssign(t, s, admin, other, b.ID)
	file := workflowTestUpload(t, s, owner, view.AssignmentID)
	foreign := workflowTestUpload(t, s, other, otherView.AssignmentID)
	for _, ids := range [][]string{nil, {file.ID, file.ID}, {file.ID, foreign.ID}, {strings.Repeat("a", 64)}, {"../outside"}, {file.ID, "a", "b", "c", "d", "e"}} {
		_, err := s.Submit(ctx, owner, view.AssignmentID, 1, ids)
		workflowTestStatus(t, err, 400)
	}
	_, err := s.Submit(ctx, owner, view.AssignmentID, 99, []string{file.ID})
	workflowTestStatus(t, err, 409)
	var stored model.MailboxAttachment
	require.NoError(t, s.DB.Where("id = ?", file.ID).First(&stored).Error)
	require.Zero(t, stored.SubmissionID)
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxSubmission{}).Count(&count).Error)
	require.Zero(t, count)
	submission, err := s.Submit(ctx, owner, view.AssignmentID, 1, []string{file.ID})
	require.NoError(t, err)
	_, err = s.Submit(ctx, owner, view.AssignmentID, 1, []string{file.ID})
	workflowTestStatus(t, err, 409)
	for _, input := range []ReviewInput{{Version: 1, Status: StatusRejected}, {Version: 1, Status: StatusRejected, Reason: " \t"}, {Version: 1, Status: StatusApproved, Reason: strings.Repeat("x", 2001)}, {Version: 1, Status: "pending"}} {
		workflowTestStatus(t, s.Review(ctx, admin, submission.ID, input), 400)
	}
	workflowTestAssign(t, s, admin, other, a.ID)
	workflowTestStatus(t, s.Review(ctx, admin, submission.ID, ReviewInput{Version: 1, Status: StatusApproved}), 404)
	var persisted model.MailboxSubmission
	require.NoError(t, s.DB.First(&persisted, submission.ID).Error)
	require.Equal(t, StatusPending, persisted.Status)
}

func TestMailboxWorkflowConcurrentCAS(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	ctx := context.Background()
	a := workflowTestAccount(t, s, "concurrent@example.test")
	compete := func(run func() error) {
		t.Helper()
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; results <- run() }()
		}
		close(start)
		wg.Wait()
		close(results)
		winners := 0
		for err := range results {
			if err == nil {
				winners++
			}
		}
		require.Equal(t, 1, winners)
	}
	compete(func() error {
		return s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{a.ID, 1}}, OperatorID: owner.OperatorID})
	})
	view, err := s.GetAccount(ctx, owner, a.ID)
	require.NoError(t, err)
	file := workflowTestUpload(t, s, owner, view.AssignmentID)
	compete(func() error { _, err := s.Submit(ctx, owner, view.AssignmentID, 1, []string{file.ID}); return err })
	var submission model.MailboxSubmission
	require.NoError(t, s.DB.First(&submission).Error)
	compete(func() error {
		return s.Review(ctx, admin, submission.ID, ReviewInput{Version: 1, Status: StatusApproved})
	})
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxSubmission{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	view, err = s.GetAccount(ctx, owner, a.ID)
	require.NoError(t, err)
	require.Equal(t, StatusApproved, view.Status)
}

func TestMailboxWorkflowAuthorizationAndPagination(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		a := workflowTestAccount(t, s, fmt.Sprintf("page-%d@example.test", i))
		workflowTestAssign(t, s, admin, owner, a.ID)
	}
	page, err := s.ListAccounts(ctx, owner, ListQuery{PageSize: 2})
	require.NoError(t, err)
	require.EqualValues(t, 3, page.Total)
	require.Len(t, page.Items, 2)
	require.True(t, page.HasMore)
	page2, err := s.ListAccounts(ctx, owner, ListQuery{Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.Len(t, page2.Items, 1)
	require.False(t, page2.HasMore)
	pageOther, err := s.ListAccounts(ctx, other, ListQuery{OperatorID: owner.OperatorID})
	require.NoError(t, err)
	require.Zero(t, pageOther.Total)
	workflowTestStatus(t, s.Assign(ctx, owner, AssignInput{}), 403)
	workflowTestStatus(t, s.Review(ctx, owner, 1, ReviewInput{}), 403)
	_, err = s.Submit(ctx, admin, 1, 1, nil)
	workflowTestStatus(t, err, 403)
	require.NoError(t, s.DB.Where("token_hash = ?", owner.SessionHash).Delete(&model.MailboxSession{}).Error)
	_, err = s.ListAccounts(ctx, owner, ListQuery{})
	workflowTestStatus(t, err, 401)
	_, err = s.ListSubmissions(ctx, owner, ListQuery{})
	workflowTestStatus(t, err, 401)
	_, err = s.GetAccount(ctx, owner, page.Items[0].ID)
	workflowTestStatus(t, err, 401)
	_, err = s.Submit(ctx, owner, 1, 1, nil)
	workflowTestStatus(t, err, 401)
	require.NoError(t, s.DB.Model(&model.User{}).Where("id = ?", admin.Admin.UserID).Update("authorization_version", gorm.Expr("authorization_version + 1")).Error)
	workflowTestStatus(t, s.Assign(ctx, admin, AssignInput{}), 401)
}

func TestMailboxWorkflowConcurrentRecallAndReview(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		a := workflowTestAccount(t, s, fmt.Sprintf("recall-review-%d@example.test", i))
		workflowTestAssign(t, s, admin, owner, a.ID)
		submission := workflowTestSubmission(t, s, owner, a.ID)
		view, err := s.GetAccount(ctx, admin, a.ID)
		require.NoError(t, err)
		start := make(chan struct{})
		results := make(chan error, 2)
		go func() {
			<-start
			results <- s.Review(ctx, admin, submission.ID, ReviewInput{Version: 1, Status: StatusApproved})
		}()
		go func() {
			<-start
			results <- s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{a.ID, view.Version}}, OperatorID: 0})
		}()
		close(start)
		winners := 0
		for j := 0; j < 2; j++ {
			if <-results == nil {
				winners++
			}
		}
		require.Equal(t, 1, winners)
		current, err := s.GetAccount(ctx, admin, a.ID)
		require.NoError(t, err)
		var stored model.MailboxSubmission
		require.NoError(t, s.DB.First(&stored, submission.ID).Error)
		if current.Status == "unassigned" {
			require.Equal(t, StatusPending, stored.Status)
		} else {
			require.Equal(t, StatusApproved, current.Status)
			require.Equal(t, StatusApproved, stored.Status)
		}
	}
}

func TestMailboxWorkflowAuditFailureRollsBackMutations(t *testing.T) {
	for _, operation := range []string{"submit", "review"} {
		t.Run(operation, func(t *testing.T) {
			s, admin, owner, _ := workflowTestService(t)
			ctx := context.Background()
			a := workflowTestAccount(t, s, "rollback@example.test")
			view := workflowTestAssign(t, s, admin, owner, a.ID)
			file := workflowTestUpload(t, s, owner, view.AssignmentID)
			var submission *SubmissionView
			var err error
			if operation == "review" {
				submission, err = s.Submit(ctx, owner, view.AssignmentID, 1, []string{file.ID})
				require.NoError(t, err)
			}
			before, err := s.GetAccount(ctx, owner, a.ID)
			require.NoError(t, err)
			callback := "mailbox_workflow_test_reject_audit"
			require.NoError(t, s.DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == "mailbox_audits" {
					tx.AddError(errors.New("synthetic audit failure"))
				}
			}))
			t.Cleanup(func() { _ = s.DB.Callback().Create().Remove(callback) })
			if operation == "submit" {
				_, err = s.Submit(ctx, owner, view.AssignmentID, 1, []string{file.ID})
			} else {
				err = s.Review(ctx, admin, submission.ID, ReviewInput{Version: 1, Status: StatusApproved})
			}
			require.Error(t, err)
			after, err := s.GetAccount(ctx, owner, a.ID)
			require.NoError(t, err)
			require.Equal(t, before, after)
			var stored model.MailboxAttachment
			require.NoError(t, s.DB.Where("id = ?", file.ID).First(&stored).Error)
			if operation == "submit" {
				require.Zero(t, stored.SubmissionID)
				require.Equal(t, file.ExpiresAt, stored.ExpiresAt)
				var count int64
				require.NoError(t, s.DB.Model(&model.MailboxSubmission{}).Count(&count).Error)
				require.Zero(t, count)
			} else {
				var persisted model.MailboxSubmission
				require.NoError(t, s.DB.First(&persisted, submission.ID).Error)
				require.Equal(t, StatusPending, persisted.Status)
				require.EqualValues(t, 1, persisted.Version)
				require.Zero(t, persisted.ReviewedAt)
			}
			_, _, err = s.ReadAttachment(ctx, owner, file.ID)
			require.NoError(t, err)
		})
	}
}
