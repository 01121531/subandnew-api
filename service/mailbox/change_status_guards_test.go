package mailbox

import (
	"context"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestChangeStatusReviewRollbackAndInvalidatedAssignment(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	ctx := context.Background()
	a := workflowTestAccount(t, s, "review-status@example.test")
	b := workflowTestAccount(t, s, "stale-status@example.test")
	workflowTestAssign(t, s, admin, owner, a.ID)
	second := workflowTestAssign(t, s, admin, owner, b.ID)
	submission := workflowTestSubmission(t, s, owner, a.ID)
	first, err := s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	input := ChangeStatusInput{Items: []ChangeStatusItem{statusItem(first), statusItem(second)}, Status: StatusRejected, Reason: "rollback"}
	input.Items[1].Version++
	workflowTestStatus(t, s.ChangeStatus(ctx, admin, input), 409)
	var unchanged model.MailboxSubmission
	require.NoError(t, s.DB.First(&unchanged, submission.ID).Error)
	require.Equal(t, StatusPending, unchanged.Status)
	require.Empty(t, unchanged.ReviewReason)
	input.Items = input.Items[:1]
	require.NoError(t, s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{ID: a.ID, Version: first.Version}}, OperatorID: 0}))
	workflowTestStatus(t, s.ChangeStatus(ctx, admin, input), 404)
	input.Items = []ChangeStatusItem{statusItem(second), statusItem(second)}
	workflowTestStatus(t, s.ChangeStatus(ctx, admin, input), 400)
	input.Items = make([]ChangeStatusItem, 1001)
	workflowTestStatus(t, s.ChangeStatus(ctx, admin, input), 400)
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxAudit{}))
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxAudit{}))
}
