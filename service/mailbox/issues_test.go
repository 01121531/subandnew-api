package mailbox

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
)

func issueResolution(view *IssueView) ResolveIssueInput {
	return ResolveIssueInput{Version: view.Version, AccountVersion: view.AccountVersion, AssignmentVersion: view.AssignmentVersion, Resolution: "resume", Reply: "Fixed; please try again."}
}

func TestMailboxIssuePauseResumeAndIsolation(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := context.Background()
	account := workflowTestAccount(t, s, "issue@example.test")
	view := workflowTestAssign(t, s, admin, owner, account.ID)
	file := workflowTestUpload(t, s, owner, view.AssignmentID)
	issue, err := s.SubmitIssue(ctx, owner, view.AssignmentID, IssueInput{Version: view.AssignmentVersion, Kind: "email_login", Description: "Cannot sign in", AttachmentIDs: []string{file.ID}}, "refund")
	require.NoError(t, err)
	require.True(t, issue.AssignmentActive)
	require.Equal(t, view.AssignmentVersion, issue.SubmittedVersion)
	require.Len(t, issue.Attachments, 1)
	_, err = s.Credentials(ctx, owner, account.ID, "password")
	workflowTestStatus(t, err, 403)
	page, err := s.ListAccounts(ctx, owner, ListQuery{Status: "actionable"})
	require.NoError(t, err)
	require.Empty(t, page.Items)
	_, err = s.Submit(ctx, owner, view.AssignmentID, issue.AssignmentVersion, []string{file.ID})
	workflowTestStatus(t, err, 409)
	_, err = s.GetIssue(ctx, other, issue.ID, "refund")
	workflowTestStatus(t, err, 404)
	_, _, err = s.ReadAttachment(ctx, other, file.ID)
	workflowTestStatus(t, err, 404)
	_, err = s.GetIssue(ctx, owner, issue.ID, "opening")
	workflowTestStatus(t, err, 404)
	require.NoError(t, s.ResolveIssue(ctx, admin, issue.ID, issueResolution(issue), "refund"))
	current, err := s.GetAccount(ctx, owner, account.ID)
	require.NoError(t, err)
	require.Equal(t, StatusPending, current.Status)
	_, err = s.Submit(ctx, owner, current.AssignmentID, current.AssignmentVersion, []string{file.ID})
	workflowTestStatus(t, err, 400)
	second, err := s.SubmitIssue(ctx, owner, current.AssignmentID, IssueInput{Version: current.AssignmentVersion, Kind: "otp", Description: "Code rejected"}, "refund")
	require.NoError(t, err)
	resolution := issueResolution(second)
	resolution.Resolution = "recall"
	require.NoError(t, s.ResolveIssue(ctx, admin, second.ID, resolution, "refund"))
	workflowTestAssign(t, s, admin, other, account.ID)
	_, err = s.GetIssue(ctx, other, issue.ID, "refund")
	workflowTestStatus(t, err, 404)
	_, _, err = s.ReadAttachment(ctx, owner, file.ID)
	require.NoError(t, err)
	history, err := s.ListIssues(ctx, owner, ListQuery{Status: "processed"})
	require.NoError(t, err)
	require.EqualValues(t, 2, history.Total)
}

func TestMailboxIssueInvalidationAndConcurrentSubmit(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := context.Background()
	account := workflowTestAccount(t, s, "concurrent-issue@example.test")
	view := workflowTestAssign(t, s, admin, owner, account.ID)
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for range 6 {
		wg.Go(func() {
			_, err := s.SubmitIssue(ctx, owner, view.AssignmentID, IssueInput{Version: view.AssignmentVersion, Kind: "other", Description: "Problem"}, "refund")
			results <- err
		})
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
	page, err := s.ListIssues(ctx, owner, ListQuery{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	issue := page.Items[0]
	workflowTestAssign(t, s, admin, other, account.ID)
	invalid, err := s.GetIssue(ctx, owner, issue.ID, "refund")
	require.NoError(t, err)
	require.Equal(t, "invalidated", invalid.Status)
	require.False(t, invalid.AssignmentActive)
	require.Error(t, s.ResolveIssue(ctx, admin, issue.ID, issueResolution(invalid), "refund"))
	current, err := s.GetAccount(ctx, other, account.ID)
	require.NoError(t, err)
	issue2, err := s.SubmitIssue(ctx, other, current.AssignmentID, IssueInput{Version: current.AssignmentVersion, Kind: "other", Description: "Problem"}, "refund")
	require.NoError(t, err)
	var op model.MailboxOperator
	require.NoError(t, s.DB.First(&op, other.OperatorID).Error)
	_, err = s.SaveOperator(ctx, admin, op.ID, OperatorInput{Username: op.Username, DisplayName: op.DisplayName, Version: op.Version, Enabled: false})
	require.NoError(t, err)
	invalid, err = s.GetIssue(ctx, admin, issue2.ID, "refund")
	require.NoError(t, err)
	require.Equal(t, "invalidated", invalid.Status)
}

func TestMailboxIssueReplacesEncryptedCredentialsAtomically(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	cipher, err := managedinstance.NewCredentialCipher(bytes.Repeat([]byte{7}, 32), "test-issue")
	require.NoError(t, err)
	s.Cipher = func() (*managedinstance.CredentialCipher, error) { return cipher, nil }
	ctx := context.Background()
	_, err = s.Import(ctx, admin, "text", []byte("test@example.test----old password----"+mailboxImportTestSecret+"----4242424242424242----12/39"), "opening")
	require.NoError(t, err)
	page, err := s.ListAccounts(ctx, admin, ListQuery{AccountType: "opening"})
	require.NoError(t, err)
	a := page.Items[0]
	require.NoError(t, s.Assign(ctx, admin, AssignInput{AccountType: "opening", Items: []AssignItem{{ID: a.ID, Version: a.Version}}, OperatorID: owner.OperatorID}))
	current, err := s.GetAccount(ctx, owner, a.ID, "opening")
	require.NoError(t, err)
	issue, err := s.SubmitIssue(ctx, owner, current.AssignmentID, IssueInput{Version: current.AssignmentVersion, Kind: "card", Description: "Card unavailable"}, "opening")
	require.NoError(t, err)
	password, pan, expiry := " new password with spaces ", "5555555555554444", "12/39"
	input := issueResolution(issue)
	input.Credentials = &IssueCredentials{Password: &password, CardNumber: &pan, CardExpiry: &expiry}
	stale := input
	stale.AccountVersion--
	require.Error(t, s.ResolveIssue(ctx, admin, issue.ID, stale, "opening"))
	require.NoError(t, s.ResolveIssue(ctx, admin, issue.ID, input, "opening"))
	credential, err := s.Credentials(ctx, owner, a.ID, "password", "opening")
	require.NoError(t, err)
	require.Equal(t, password, credential.Password)
	card, err := s.Credentials(ctx, owner, a.ID, "card", "opening")
	require.NoError(t, err)
	require.Equal(t, pan, card.CardNumber)
	var stored model.MailboxAccount
	require.NoError(t, s.DB.First(&stored, a.ID).Error)
	require.NotContains(t, stored.Ciphertext, password)
	require.NotContains(t, stored.CardCiphertext, pan)
	var audits []model.MailboxAudit
	require.NoError(t, s.DB.Find(&audits).Error)
	encoded, err := json.Marshal(audits)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), password)
	require.NotContains(t, string(encoded), pan)
	view, err := s.GetIssue(ctx, owner, issue.ID, "opening")
	require.NoError(t, err)
	encoded, err = json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), password)
	require.NotContains(t, string(encoded), pan)
}

func TestMailboxIssueValidationAndMigration(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxIssue{}, &model.MailboxAttachment{}))
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxIssue{}, &model.MailboxAttachment{}))
	a := workflowTestAccount(t, s, "validation@example.test")
	view := workflowTestAssign(t, s, admin, owner, a.ID)
	for _, input := range []IssueInput{{Kind: "card", Description: "Not for refund"}, {Kind: "other", Description: " "}, {Kind: "invalid", Description: "Invalid"}} {
		input.Version = view.AssignmentVersion
		_, err := s.SubmitIssue(context.Background(), owner, view.AssignmentID, input, "refund")
		workflowTestStatus(t, err, 400)
	}
}

func TestMailboxIssueReviewPermissionDoesNotGrantCredentialsOrRecall(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	master := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = master })
	require.NoError(t, s.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(s.DB))
	u := model.User{Username: "issue-review-only", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, s.DB.Create(&u).Error)
	require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: {"review": true}}))
	access, err := authz.LoadDataAccess(s.DB, u.Id)
	require.NoError(t, err)
	reviewer := Actor{Admin: access}
	ctx := context.Background()
	a := workflowTestAccount(t, s, "review-only@example.test")
	v := workflowTestAssign(t, s, admin, owner, a.ID)
	file := workflowTestUpload(t, s, owner, v.AssignmentID)
	issue, err := s.SubmitIssue(ctx, owner, v.AssignmentID, IssueInput{Version: v.AssignmentVersion, Kind: "other", Description: "Synthetic problem", AttachmentIDs: []string{file.ID}}, "refund")
	require.NoError(t, err)
	_, err = s.ListIssues(ctx, reviewer, ListQuery{})
	require.NoError(t, err)
	_, _, err = s.ReadAttachment(ctx, reviewer, file.ID)
	require.NoError(t, err)
	input := issueResolution(issue)
	password := "replacement"
	input.Credentials = &IssueCredentials{Password: &password}
	workflowTestStatus(t, s.ResolveIssue(ctx, reviewer, issue.ID, input, "refund"), 403)
	input.Credentials = nil
	input.Resolution = "recall"
	workflowTestStatus(t, s.ResolveIssue(ctx, reviewer, issue.ID, input, "refund"), 403)
	input.Resolution = "resume"
	require.NoError(t, s.ResolveIssue(ctx, reviewer, issue.ID, input, "refund"))
	_, err = s.Credentials(ctx, reviewer, a.ID, "password")
	workflowTestStatus(t, err, 403)
	_, err = s.ImportOptions(reviewer)
	workflowTestStatus(t, err, 403)
}

func TestMailboxIssueAndNormalSubmissionAreMutuallyExclusive(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	a := workflowTestAccount(t, s, "competing-submit@example.test")
	v := workflowTestAssign(t, s, admin, owner, a.ID)
	file := workflowTestUpload(t, s, owner, v.AssignmentID)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := s.SubmitIssue(context.Background(), owner, v.AssignmentID, IssueInput{Version: v.AssignmentVersion, Kind: "other", Description: "Issue", AttachmentIDs: []string{file.ID}}, "refund")
		results <- err
	})
	wg.Go(func() {
		_, err := s.Submit(context.Background(), owner, v.AssignmentID, v.AssignmentVersion, []string{file.ID})
		results <- err
	})
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
	var attachment model.MailboxAttachment
	require.NoError(t, s.DB.First(&attachment, "id = ?", file.ID).Error)
	require.NotEqual(t, attachment.IssueID > 0, attachment.SubmissionID > 0)
	now := s.Now()
	s.Now = func() time.Time { return now.Add(366 * 24 * time.Hour) }
	_, _, err := s.ReadAttachment(context.Background(), admin, file.ID)
	require.Error(t, err)
}

func TestMailboxIssueClearsUnclaimedCVVAndRestoringDoesNotReissue(t *testing.T) {
	s, admin, owner := cvvTestService(t)
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxAttachment{}))
	ctx := context.Background()
	_, err := s.Import(ctx, admin, "text", []byte("cvv-issue@example.test----password----"+mailboxImportTestSecret+"----4242424242424242----12/39----007"), "opening")
	require.NoError(t, err)
	page, err := s.ListAccounts(ctx, admin, ListQuery{AccountType: "opening"})
	require.NoError(t, err)
	v := cvvTestAssign(t, s, admin, owner, page.Items[0].ID)
	issue, err := s.SubmitIssue(ctx, owner, v.AssignmentID, IssueInput{Version: v.AssignmentVersion, Kind: "card", Description: "Test unavailable card"}, "opening")
	require.NoError(t, err)
	_, err = s.Credentials(ctx, owner, v.ID, "cvv", "opening")
	require.Error(t, err)
	require.NoError(t, s.ResolveIssue(ctx, admin, issue.ID, issueResolution(issue), "opening"))
	_, err = s.Credentials(ctx, owner, v.ID, "cvv", "opening")
	require.NoError(t, err)
}
