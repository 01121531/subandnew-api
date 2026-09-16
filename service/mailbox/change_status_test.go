package mailbox

import (
	"context"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func statusItem(v *AccountView) ChangeStatusItem {
	return ChangeStatusItem{v.ID, v.Version, v.AssignmentID, v.AssignmentVersion}
}

func TestChangeStatusLifecycle(t *testing.T) {
	for _, pool := range []string{AccountTypeRefund, AccountTypeOpening} {
		t.Run(pool, func(t *testing.T) {
			s, admin, owner, _ := workflowTestService(t)
			a := model.MailboxAccount{AccountType: pool, Email: "status@example.test", Ciphertext: "synthetic", KeyVersion: "test", Version: 1}
			require.NoError(t, s.DB.Create(&a).Error)
			ctx := context.Background()
			require.NoError(t, s.Assign(ctx, admin, AssignInput{AccountType: pool, Items: []AssignItem{{ID: a.ID, Version: 1}}, OperatorID: owner.OperatorID}))
			get := func(actor Actor) *AccountView {
				v, err := s.GetAccount(ctx, actor, a.ID, pool)
				require.NoError(t, err)
				return v
			}
			submit := func() *SubmissionView {
				v := get(owner)
				result, err := s.SubmitWithRemark(ctx, owner, v.AssignmentID, v.AssignmentVersion, nil, "test", pool)
				require.NoError(t, err)
				return result
			}
			v := get(admin)
			change := func(target string) error {
				return s.ChangeStatus(ctx, admin, ChangeStatusInput{AccountType: pool, Items: []ChangeStatusItem{statusItem(v)}, Status: target, Reason: " correction "})
			}
			workflowTestStatus(t, change(StatusApproved), 409)
			require.NoError(t, change(StatusRejected))
			v = get(admin)
			require.NoError(t, change(StatusPending))
			submission := submit()
			v = get(admin)
			require.NoError(t, change(StatusApproved))
			denied := get(owner)
			require.False(t, denied.CredentialsAvailable)
			v = get(admin)
			require.NoError(t, change(StatusPending))
			current := get(owner)
			require.True(t, current.CredentialsAvailable)
			var old model.MailboxSubmission
			require.NoError(t, s.DB.First(&old, submission.ID).Error)
			require.Equal(t, StatusApproved, old.Status)
			require.Equal(t, "correction", old.ReviewReason)
			next := submit()
			require.NotEqual(t, submission.ID, next.ID)
			var audit model.MailboxAudit
			require.NoError(t, s.DB.Where("action = ?", "change_status").Order("id DESC").First(&audit).Error)
			require.Equal(t, StatusApproved, audit.FromStatus)
			require.Equal(t, StatusPending, audit.ToStatus)
			require.Equal(t, "correction", audit.Reason)
		})
	}
}

func TestChangeStatusAtomicAndGuards(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	ctx := context.Background()
	a := workflowTestAccount(t, s, "first@example.test")
	b := workflowTestAccount(t, s, "second@example.test")
	first := workflowTestAssign(t, s, admin, owner, a.ID)
	second := workflowTestAssign(t, s, admin, owner, b.ID)
	input := ChangeStatusInput{Items: []ChangeStatusItem{statusItem(first), statusItem(second)}, Status: StatusRejected, Reason: "test"}
	input.Items[1].AssignmentVersion++
	workflowTestStatus(t, s.ChangeStatus(ctx, admin, input), 409)
	after, err := s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	require.Equal(t, *first, *after)
	workflowTestStatus(t, s.ChangeStatus(ctx, owner, input), 403)
	input.Items[1] = statusItem(second)
	input.AccountType = AccountTypeOpening
	workflowTestStatus(t, s.ChangeStatus(ctx, admin, input), 404)
	input.AccountType = AccountTypeRefund
	input.Reason = " "
	workflowTestStatus(t, s.ChangeStatus(ctx, admin, input), 400)
	input.Reason = "test"
	require.NoError(t, s.ChangeStatus(ctx, admin, input))
	workflowTestStatus(t, s.ChangeStatus(ctx, admin, input), 409)
	for _, from := range []string{"unassigned", StatusIssuePending, StatusPending, StatusRejected, StatusApproved} {
		require.False(t, validStatusChange(from, StatusSubmitted))
	}
}
