package mailbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Seed history that predates the protected-submission assignment guard. Do not
// weaken workflowTestAssign: ordinary fixtures must still exercise Assign.
func legacyTestReassign(t *testing.T, s *Service, admin, operator Actor, accountID int64, pools ...string) *AccountView {
	t.Helper()
	local, err := s.withAccountType("", pools...)
	require.NoError(t, err)
	require.NoError(t, local.DB.Transaction(func(tx *gorm.DB) error {
		var a model.MailboxAccount
		if err := tx.First(&a, accountID).Error; err != nil {
			return err
		}
		now := s.Now().Unix()
		if a.ActiveAssignmentID != 0 {
			if err := tx.Model(&model.MailboxAssignment{}).Where("id = ?", a.ActiveAssignmentID).Updates(map[string]any{"revoked_at": now, "version": gorm.Expr("version + 1"), "updated_at": now}).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.MailboxIssue{}).Where("assignment_id = ? AND status = ?", a.ActiveAssignmentID, "pending").Updates(map[string]any{"status": "invalidated", "open_assignment_id": nil, "version": gorm.Expr("version + 1")}).Error; err != nil {
				return err
			}
		}
		assignment := model.MailboxAssignment{AccountID: a.ID, OperatorID: operator.OperatorID, Status: StatusPending, Version: 1, AssignedBy: admin.Admin.UserID, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&assignment).Error; err != nil {
			return err
		}
		return tx.Model(&a).Updates(map[string]any{"active_assignment_id": assignment.ID, "version": gorm.Expr("version + 1"), "updated_at": now}).Error
	}))
	view, err := local.GetAccount(context.Background(), operator, accountID, local.pool())
	require.NoError(t, err)
	return view
}

func repairTestFixture(t *testing.T, s *Service, owner, other Actor, pool, status, email string, recalled bool) RepairCandidate {
	t.Helper()
	s.DB.NowFunc = s.Now
	a := model.MailboxAccount{AccountType: pool, Email: email, Version: 4, Ciphertext: "unchanged-secret", KeyVersion: "test"}
	require.NoError(t, s.DB.Create(&a).Error)
	at := s.Now().Unix()
	assignmentStatus := StatusSubmitted
	if status == StatusApproved {
		assignmentStatus = StatusApproved
	}
	original := model.MailboxAssignment{AccountID: a.ID, OperatorID: owner.OperatorID, Status: assignmentStatus, Version: 7, CreatedAt: at - 100, UpdatedAt: at - 20, RevokedAt: at - 20}
	require.NoError(t, s.DB.Create(&original).Error)
	sub := model.MailboxSubmission{AssignmentID: original.ID, OperatorID: owner.OperatorID, Status: status, Version: 3, CreatedAt: at - 80, Remark: "preserve remark"}
	if status == StatusApproved {
		sub.ReviewedAt, sub.ReviewedBy, sub.ReviewReason = at-50, 1, "preserve review"
	}
	require.NoError(t, s.DB.Create(&sub).Error)
	require.NoError(t, s.DB.Create(&model.MailboxAttachment{ID: fmt.Sprintf("original-%d", a.ID), AssignmentID: original.ID, OperatorID: owner.OperatorID, SubmissionID: sub.ID, StorageKey: "preserve.png", ContentType: "image/png", CreatedAt: at - 90, ExpiresAt: at + 86400}).Error)
	if !recalled {
		current := model.MailboxAssignment{AccountID: a.ID, OperatorID: other.OperatorID, Status: StatusPending, Version: 2, CreatedAt: at - 20, UpdatedAt: at - 20}
		require.NoError(t, s.DB.Create(&current).Error)
		a.ActiveAssignmentID = current.ID
		require.NoError(t, s.DB.Model(&a).Update("active_assignment_id", current.ID).Error)
	}
	local, err := s.withAccountType(pool)
	require.NoError(t, err)
	candidate, err := local.repairCandidate(&a, sub.ID)
	require.NoError(t, err)
	require.True(t, candidate.CanRepair, candidate.ConflictCode)
	return *candidate
}

type repairTestState struct {
	Accounts    []model.MailboxAccount
	Assignments []model.MailboxAssignment
	Submissions []model.MailboxSubmission
	Attachments []model.MailboxAttachment
	Issues      []model.MailboxIssue
	Audits      []model.MailboxAudit
}

func repairTestSnapshot(t *testing.T, s *Service) repairTestState {
	t.Helper()
	var state repairTestState
	for _, rows := range []any{&state.Accounts, &state.Assignments, &state.Submissions, &state.Attachments, &state.Issues, &state.Audits} {
		require.NoError(t, s.DB.Order("id").Find(rows).Error)
	}
	return state
}

func TestMailboxAssignmentRepairRestore(t *testing.T) {
	for _, pool := range []string{AccountTypeRefund, AccountTypeOpening} {
		for _, status := range []string{StatusPending, StatusApproved} {
			for _, recalled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/recalled=%t", pool, status, recalled), func(t *testing.T) {
					s, admin, owner, other := workflowTestService(t)
					ctx := context.Background()
					candidate := repairTestFixture(t, s, owner, other, pool, status, "restore@example.test", recalled)
					before := repairTestSnapshot(t, s)
					page, err := s.ListAssignmentRepairs(ctx, admin, ListQuery{AccountType: pool, Search: "RESTORE@", OperatorID: owner.OperatorID, PageSize: 1})
					require.NoError(t, err)
					require.EqualValues(t, 1, page.Total)
					require.Equal(t, []RepairCandidate{candidate}, page.Items)
					input := RepairInput{AccountType: pool, Reason: "  restore historical owner  ", Items: []RepairItem{candidate.RepairItem}}
					require.NoError(t, s.RepairAssignments(ctx, admin, input))
					after := repairTestSnapshot(t, s)
					expectedStatus := StatusSubmitted
					if status == StatusApproved {
						expectedStatus = StatusApproved
					}
					expectedAccount := before.Accounts[0]
					expectedAccount.ActiveAssignmentID = candidate.OriginalAssignmentID
					expectedAccount.Version++
					expectedAccount.UpdatedAt = s.Now().Unix()
					require.Equal(t, []model.MailboxAccount{expectedAccount}, after.Accounts)
					expectedAssignments := append([]model.MailboxAssignment(nil), before.Assignments...)
					for i := range expectedAssignments {
						expectedAssignments[i].Version++
						expectedAssignments[i].UpdatedAt = s.Now().Unix()
						if expectedAssignments[i].ID == candidate.OriginalAssignmentID {
							expectedAssignments[i].RevokedAt = 0
							expectedAssignments[i].Status = expectedStatus
						} else {
							expectedAssignments[i].RevokedAt = s.Now().Unix()
						}
					}
					require.Equal(t, expectedAssignments, after.Assignments)
					require.Equal(t, before.Submissions, after.Submissions)
					require.Equal(t, before.Attachments, after.Attachments)
					require.Equal(t, before.Issues, after.Issues)
					require.Len(t, after.Audits, 2)
					for _, audit := range after.Audits {
						require.Equal(t, pool, audit.AccountType)
						require.Equal(t, admin.Admin.UserID, audit.AdminID)
						require.Equal(t, owner.OperatorID, audit.TargetOperatorID)
						require.Equal(t, candidate.AccountID, audit.AccountID)
						require.Equal(t, candidate.OriginalAssignmentID, audit.AssignmentID)
					}
					require.Equal(t, "repair_assignment", after.Audits[0].Action)
					require.Equal(t, "restore historical owner", after.Audits[0].Reason)
					require.Equal(t, candidate.CurrentStatus, after.Audits[0].FromStatus)
					require.Equal(t, expectedStatus, after.Audits[0].ToStatus)
					var links map[string]int64
					require.NoError(t, json.Unmarshal([]byte(after.Audits[1].Reason), &links))
					require.Equal(t, map[string]int64{"OriginalAssignmentID": candidate.OriginalAssignmentID, "RevokedAssignmentID": candidate.CurrentAssignmentID, "SubmissionID": candidate.SubmissionID, "OriginalRevokedAt": candidate.OriginalRevokedAt}, links)
					view, err := s.GetAccount(ctx, owner, candidate.AccountID, pool)
					require.NoError(t, err)
					require.Equal(t, candidate.OriginalAssignmentID, view.AssignmentID)
					require.Equal(t, expectedStatus, view.Status)
					_, err = s.GetAccount(ctx, other, candidate.AccountID, pool)
					workflowTestStatus(t, err, 404)
					page, err = s.ListAssignmentRepairs(ctx, admin, ListQuery{AccountType: pool})
					require.NoError(t, err)
					require.Zero(t, page.Total)
					operatorTestError(t, s.RepairAssignments(ctx, admin, input), 409, "mailbox_version_conflict")
					require.Equal(t, after, repairTestSnapshot(t, s))
				})
			}
		}
	}
}

func TestMailboxAssignmentRepairConflictsAfterPreview(t *testing.T) {
	for _, pool := range []string{AccountTypeRefund, AccountTypeOpening} {
		for _, mode := range []string{"submission", "original_newer_submission", "issue", "attachment", "deleted_attachment", "disabled", "archived", "status_mutation", "operator_mismatch"} {
			t.Run(pool+"/"+mode, func(t *testing.T) {
				s, admin, owner, other := workflowTestService(t)
				candidate := repairTestFixture(t, s, owner, other, pool, StatusPending, "conflict@example.test", false)
				code := "mailbox_repair_later_activity"
				switch mode {
				case "submission", "original_newer_submission":
					id, operatorID := candidate.CurrentAssignmentID, other.OperatorID
					if mode == "original_newer_submission" {
						id, operatorID = candidate.OriginalAssignmentID, owner.OperatorID
					}
					require.NoError(t, s.DB.Create(&model.MailboxSubmission{AssignmentID: id, OperatorID: operatorID, Status: StatusRejected, CreatedAt: s.Now().Unix()}).Error)
				case "issue":
					require.NoError(t, s.DB.Create(&model.MailboxIssue{AccountID: candidate.AccountID, AssignmentID: candidate.CurrentAssignmentID, OperatorID: other.OperatorID, Status: "resolved", Kind: "other"}).Error)
				case "attachment", "deleted_attachment":
					file := model.MailboxAttachment{ID: "later-file", AssignmentID: candidate.CurrentAssignmentID, OperatorID: other.OperatorID, StorageKey: "later.png", ContentType: "image/png"}
					if mode == "deleted_attachment" {
						file.DeletedAt = s.Now().Unix()
					}
					require.NoError(t, s.DB.Create(&file).Error)
				case "disabled":
					code = "mailbox_repair_operator_disabled"
					require.NoError(t, s.DB.Model(&model.MailboxOperator{}).Where("id = ?", owner.OperatorID).Update("enabled", false).Error)
				case "archived":
					code = "mailbox_account_archived"
					require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", candidate.AccountID).Update("archived_at", s.Now().Unix()).Error)
				case "status_mutation":
					code = "mailbox_repair_ambiguous"
					require.NoError(t, s.DB.Create(&model.MailboxAudit{AccountType: pool, AccountID: candidate.AccountID, AssignmentID: candidate.OriginalAssignmentID, Action: "change_status", FromStatus: StatusApproved, ToStatus: StatusPending, CreatedAt: s.Now().Unix()}).Error)
				case "operator_mismatch":
					code = "mailbox_repair_ambiguous"
					require.NoError(t, s.DB.Model(&model.MailboxSubmission{}).Where("id = ?", candidate.SubmissionID).Update("operator_id", other.OperatorID).Error)
				}
				before := repairTestSnapshot(t, s)
				page, err := s.ListAssignmentRepairs(context.Background(), admin, ListQuery{AccountType: pool})
				require.NoError(t, err)
				require.Len(t, page.Items, 1)
				require.False(t, page.Items[0].CanRepair)
				require.Equal(t, code, page.Items[0].ConflictCode)
				operatorTestError(t, s.RepairAssignments(context.Background(), admin, RepairInput{AccountType: pool, Reason: "stale preview", Items: []RepairItem{candidate.RepairItem}}), 409, code)
				require.Equal(t, before, repairTestSnapshot(t, s))
			})
		}
	}
}

func TestMailboxAssignmentRepairStaleBatchAtomic(t *testing.T) {
	for _, field := range []string{"account", "original", "current", "submission", "later_activity", "audit_failure"} {
		t.Run(field, func(t *testing.T) {
			s, admin, owner, other := workflowTestService(t)
			first := repairTestFixture(t, s, owner, other, "refund", StatusApproved, "first@example.test", false)
			second := repairTestFixture(t, s, owner, other, "refund", StatusPending, "second@example.test", false)
			code := "mailbox_version_conflict"
			switch field {
			case "account":
				require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", second.AccountID).Update("version", second.AccountVersion+1).Error)
			case "original":
				require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", second.OriginalAssignmentID).Update("version", second.OriginalAssignmentVersion+1).Error)
			case "current":
				require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", second.CurrentAssignmentID).Update("version", second.CurrentAssignmentVersion+1).Error)
			case "submission":
				require.NoError(t, s.DB.Model(&model.MailboxSubmission{}).Where("id = ?", second.SubmissionID).Update("version", second.SubmissionVersion+1).Error)
			case "later_activity":
				code = "mailbox_repair_later_activity"
				require.NoError(t, s.DB.Create(&model.MailboxAttachment{ID: "batch-conflict", AssignmentID: second.CurrentAssignmentID, OperatorID: other.OperatorID, StorageKey: "draft", ContentType: "image/png"}).Error)
			case "audit_failure":
				require.NoError(t, s.DB.Exec("CREATE TRIGGER fail_repair_audit BEFORE INSERT ON mailbox_audits WHEN NEW.action = 'repair_assignment_links' BEGIN SELECT RAISE(ABORT, 'repair audit failure'); END").Error)
			}
			before := repairTestSnapshot(t, s)
			input := RepairInput{AccountType: "refund", Reason: "atomic repair", Items: []RepairItem{second.RepairItem, first.RepairItem}}
			err := s.RepairAssignments(context.Background(), admin, input)
			if field == "audit_failure" {
				require.ErrorContains(t, err, "repair audit failure")
			} else {
				operatorTestError(t, err, 409, code)
			}
			require.Equal(t, before, repairTestSnapshot(t, s))
			require.Equal(t, second.RepairItem, input.Items[0], "caller batch must not be reordered")
		})
	}
}

func TestMailboxAssignmentGuardProtectedHistory(t *testing.T) {
	for _, pool := range []string{AccountTypeRefund, AccountTypeOpening} {
		for _, status := range []string{StatusPending, StatusApproved} {
			for _, mode := range []string{"legacy", "recalled", "reopened", "unarchived"} {
				t.Run(pool+"/"+status+"/"+mode, func(t *testing.T) {
					s, admin, owner, other := workflowTestService(t)
					clean := model.MailboxAccount{AccountType: pool, Email: "clean@example.test", Version: 1}
					require.NoError(t, s.DB.Create(&clean).Error)
					candidate := repairTestFixture(t, s, owner, other, pool, status, "protected@example.test", mode == "recalled" || mode == "unarchived")
					if mode == "reopened" {
						require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", candidate.OriginalAssignmentID).Update("status", StatusPending).Error)
					}
					if mode == "unarchived" {
						require.NoError(t, s.DB.Model(&model.MailboxAccount{}).Where("id = ?", candidate.AccountID).Update("archived_at", s.Now().Unix()).Error)
						require.NoError(t, s.ArchiveAccounts(context.Background(), admin, ArchiveInput{AccountType: pool, Items: []AssignItem{{ID: candidate.AccountID, Version: candidate.AccountVersion}}}, true))
					}
					var account model.MailboxAccount
					require.NoError(t, s.DB.First(&account, candidate.AccountID).Error)
					before := repairTestSnapshot(t, s)
					err := s.Assign(context.Background(), admin, AssignInput{AccountType: pool, OperatorID: owner.OperatorID, Items: []AssignItem{{ID: account.ID, Version: account.Version}, {ID: clean.ID, Version: clean.Version}}})
					operatorTestError(t, err, 409, "mailbox_already_submitted")
					var conflicts *AssignmentConflictError
					require.ErrorAs(t, err, &conflicts)
					require.Equal(t, []AssignmentConflict{{ID: account.ID, Email: account.Email}}, conflicts.Conflicts)
					require.Equal(t, before, repairTestSnapshot(t, s))
					// Recall remains legal, but cannot erase the protection.
					require.NoError(t, s.Assign(context.Background(), admin, AssignInput{AccountType: pool, Items: []AssignItem{{ID: account.ID, Version: account.Version}}}))
					require.NoError(t, s.DB.First(&account, account.ID).Error)
					operatorTestError(t, s.Assign(context.Background(), admin, AssignInput{AccountType: pool, OperatorID: other.OperatorID, Items: []AssignItem{{ID: account.ID, Version: account.Version}}}), 409, "mailbox_already_submitted")
				})
			}
		}
	}
}

func TestMailboxAssignmentRepairConcurrentCAS(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	candidate := repairTestFixture(t, s, owner, other, "refund", StatusPending, "race@example.test", false)
	input := RepairInput{AccountType: "refund", Reason: "concurrent repair", Items: []RepairItem{candidate.RepairItem}}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- s.RepairAssignments(context.Background(), admin, input)
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	state := repairTestSnapshot(t, s)
	require.Equal(t, candidate.OriginalAssignmentID, state.Accounts[0].ActiveAssignmentID)
	require.Equal(t, candidate.AccountVersion+1, state.Accounts[0].Version)
	require.Len(t, state.Audits, 2)
}

func TestMailboxAssignmentRepairRacesAttachmentUpload(t *testing.T) {
	for _, pool := range []string{AccountTypeRefund, AccountTypeOpening} {
		t.Run(pool, func(t *testing.T) {
			s, admin, owner, other := workflowTestService(t)
			candidate := repairTestFixture(t, s, owner, other, pool, StatusPending, "upload-race@example.test", false)
			input := RepairInput{AccountType: pool, Reason: "race with draft upload", Items: []RepairItem{candidate.RepairItem}}
			png := mailboxTestPNG(t)
			start := make(chan struct{})
			repaired, uploaded := make(chan error, 1), make(chan error, 1)
			go func() { <-start; repaired <- s.RepairAssignments(context.Background(), admin, input) }()
			go func() {
				<-start
				_, err := s.UploadAttachment(context.Background(), other, candidate.CurrentAssignmentID, bytes.NewReader(png), pool)
				uploaded <- err
			}()
			close(start)
			repairErr, uploadErr := <-repaired, <-uploaded
			require.False(t, repairErr == nil && uploadErr == nil, "repair and later activity must not both commit")
			require.True(t, repairErr == nil || uploadErr == nil, "one operation must succeed: repair=%v upload=%v", repairErr, uploadErr)
			state := repairTestSnapshot(t, s)
			if repairErr == nil {
				require.Equal(t, candidate.OriginalAssignmentID, state.Accounts[0].ActiveAssignmentID)
				require.Len(t, state.Attachments, 1, "repair must not leave a draft on the revoked assignment")
				require.Len(t, state.Audits, 2)
			} else {
				require.Equal(t, candidate.CurrentAssignmentID, state.Accounts[0].ActiveAssignmentID)
				require.Len(t, state.Attachments, 2)
				require.NotZero(t, state.Assignments[0].RevokedAt)
				require.Zero(t, state.Assignments[1].RevokedAt)
				for _, audit := range state.Audits {
					require.NotContains(t, audit.Action, "repair_assignment")
				}
			}
			require.Equal(t, candidate.AccountVersion+1, state.Accounts[0].Version)
		})
	}
}

func TestMailboxAssignmentGuardAllowsRejectedHistory(t *testing.T) {
	for _, pool := range []string{AccountTypeRefund, AccountTypeOpening} {
		t.Run(pool, func(t *testing.T) {
			s, admin, owner, other := workflowTestService(t)
			candidate := repairTestFixture(t, s, owner, other, pool, StatusPending, "rejected@example.test", true)
			require.NoError(t, s.DB.Model(&model.MailboxSubmission{}).Where("id = ?", candidate.SubmissionID).Update("status", StatusRejected).Error)
			require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Where("id = ?", candidate.OriginalAssignmentID).Update("status", StatusRejected).Error)
			require.NoError(t, s.Assign(context.Background(), admin, AssignInput{AccountType: pool, OperatorID: other.OperatorID, Items: []AssignItem{{ID: candidate.AccountID, Version: candidate.AccountVersion}}}))
			view, err := s.GetAccount(context.Background(), other, candidate.AccountID, pool)
			require.NoError(t, err)
			require.Equal(t, StatusPending, view.Status)
			require.NotEqual(t, candidate.OriginalAssignmentID, view.AssignmentID)
		})
	}
}

func TestMailboxAssignmentRepairValidationAndPoolIsolation(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	candidate := repairTestFixture(t, s, owner, other, "opening", StatusPending, "same@example.test", true)
	repairTestFixture(t, s, owner, other, "refund", StatusApproved, "same@example.test", true)
	input := RepairInput{AccountType: "opening", Reason: "restore", Items: []RepairItem{candidate.RepairItem}}
	before := repairTestSnapshot(t, s)
	for _, actor := range []Actor{owner, {}} {
		operatorTestError(t, s.RepairAssignments(context.Background(), actor, input), 403, "mailbox_permission_denied")
		_, err := s.ListAssignmentRepairs(context.Background(), actor, ListQuery{AccountType: "opening"})
		operatorTestError(t, err, 403, "mailbox_permission_denied")
	}
	for _, mutate := range []func(*RepairInput){
		func(q *RepairInput) { q.Reason = " \t" },
		func(q *RepairInput) { q.Reason = "invalid\x00reason" },
		func(q *RepairInput) { q.Items = nil },
		func(q *RepairInput) { q.Items = append(q.Items, q.Items[0]) },
		func(q *RepairInput) { q.Items[0].OriginalAssignmentVersion = 0 },
		func(q *RepairInput) { q.Items[0].CurrentAssignmentVersion = 1 },
	} {
		invalid := input
		invalid.Items = append([]RepairItem(nil), input.Items...)
		mutate(&invalid)
		workflowTestStatus(t, s.RepairAssignments(context.Background(), admin, invalid), 400)
	}
	wrongPool := input
	wrongPool.AccountType = "refund"
	operatorTestError(t, s.RepairAssignments(context.Background(), admin, wrongPool), 404, "mailbox_not_found")
	for _, pool := range []string{"opening", "refund"} {
		page, err := s.ListAssignmentRepairs(context.Background(), admin, ListQuery{AccountType: pool})
		require.NoError(t, err)
		require.EqualValues(t, 1, page.Total)
		require.Len(t, page.Items, 1)
		require.Equal(t, pool, page.Items[0].AccountType)
	}
	require.Equal(t, before, repairTestSnapshot(t, s))
}
