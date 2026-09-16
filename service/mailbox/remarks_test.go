package mailbox

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestRemarkEditsOwnershipHistoryAndReview(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxRemarkRevision{}))
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxRemarkRevision{}))
	ctx := context.Background()
	a := workflowTestAccount(t, s, "remark@example.test")
	v := workflowTestAssign(t, s, admin, owner, a.ID)
	original, err := s.SubmitWithRemark(ctx, owner, v.AssignmentID, v.AssignmentVersion, nil, "original")
	require.NoError(t, err)
	_, err = s.EditRemark(ctx, other, original.ID, RemarkInput{Version: original.Version, Remark: "attack"})
	workflowTestStatus(t, err, 404)
	_, err = s.EditRemark(ctx, owner, original.ID, RemarkInput{Version: original.Version, Remark: " "})
	workflowTestStatus(t, err, 400)
	_, err = s.EditRemark(ctx, owner, original.ID, RemarkInput{Version: original.Version, Remark: strings.Repeat("字", 2001)})
	workflowTestStatus(t, err, 400)
	edited, err := s.EditRemark(ctx, owner, original.ID, RemarkInput{Version: original.Version, Remark: "<b>plain</b>\nupdated"})
	require.NoError(t, err)
	require.True(t, edited.CanEditRemark)
	require.Equal(t, original.CreatedAt, edited.CreatedAt)
	require.Equal(t, original.Status, edited.Status)
	_, err = s.EditRemark(ctx, admin, original.ID, RemarkInput{Version: original.Version, Remark: "stale"})
	workflowTestStatus(t, err, 409)
	require.NoError(t, s.Review(ctx, admin, original.ID, ReviewInput{Version: edited.Version, Status: StatusApproved, Reason: "approved"}))
	_, err = s.EditRemark(ctx, owner, original.ID, RemarkInput{Version: edited.Version + 1, Remark: "late"})
	workflowTestStatus(t, err, 403)
	approved, err := s.EditRemark(ctx, admin, original.ID, RemarkInput{Version: edited.Version + 1, Remark: "admin correction"})
	require.NoError(t, err)
	require.Equal(t, StatusApproved, approved.Status)
	require.Equal(t, "approved", approved.ReviewReason)
	current, err := s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	require.NoError(t, s.Assign(ctx, admin, AssignInput{Items: []AssignItem{{ID: a.ID, Version: current.Version}}, OperatorID: 0}))
	current, err = s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	require.NoError(t, s.ArchiveAccounts(ctx, admin, ArchiveInput{Items: []AssignItem{{ID: a.ID, Version: current.Version}}}, false))
	_, err = s.EditRemark(ctx, admin, original.ID, RemarkInput{Version: approved.Version, Remark: "archived correction"})
	require.NoError(t, err)
	history, err := s.RemarkHistory(ctx, owner, original.ID, ListQuery{})
	require.NoError(t, err)
	require.EqualValues(t, 3, history.Total)
	require.Equal(t, "original", history.Items[2].OldRemark)
	_, err = s.RemarkHistory(ctx, other, original.ID, ListQuery{})
	workflowTestStatus(t, err, 404)
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxSubmission{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var audits []model.MailboxAudit
	require.NoError(t, s.DB.Where("action = ?", "edit_remark").Find(&audits).Error)
	for _, audit := range audits {
		require.Empty(t, audit.Reason)
		require.Empty(t, audit.ErrorCode)
	}
}

func TestRemarkOpeningExportAndConcurrentEdits(t *testing.T) {
	s, admin, owner := completedTestSetup(t)
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxRemarkRevision{}))
	_, submission := completedTestSubmit(t, s, admin, owner, "export-remark@example.test")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, remark := range []string{"first correction", "second correction"} {
		wg.Add(1)
		go func(remark string) {
			defer wg.Done()
			<-start
			_, err := s.EditRemark(t.Context(), admin, submission.ID, RemarkInput{AccountType: AccountTypeOpening, Version: submission.Version, Remark: remark})
			results <- err
		}(remark)
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	var current model.MailboxSubmission
	require.NoError(t, s.DB.First(&current, submission.ID).Error)
	data, count, err := s.ExportCompletedOpening(t.Context(), admin)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	file, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer file.Close()
	rows, err := file.GetRows("Sheet1")
	require.NoError(t, err)
	require.Contains(t, rows[1], current.Remark)
	history, err := s.RemarkHistory(t.Context(), admin, submission.ID, ListQuery{AccountType: AccountTypeOpening})
	require.NoError(t, err)
	require.EqualValues(t, 1, history.Total)
}

func TestRemarkEditLatestRejectedAndScreenshots(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	ctx := context.Background()
	require.NoError(t, s.DB.AutoMigrate(&model.MailboxRemarkRevision{}))
	a := workflowTestAccount(t, s, "screenshot-remark@example.test")
	workflowTestAssign(t, s, admin, owner, a.ID)
	submission := workflowTestSubmission(t, s, owner, a.ID)
	edited, err := s.EditRemark(ctx, owner, submission.ID, RemarkInput{Version: submission.Version, Remark: ""})
	require.NoError(t, err)
	require.Len(t, edited.Attachments, 1)
	require.NoError(t, s.Review(ctx, admin, edited.ID, ReviewInput{Version: edited.Version, Status: StatusRejected, Reason: "retry"}))
	edited, err = s.EditRemark(ctx, owner, edited.ID, RemarkInput{Version: edited.Version + 1, Remark: "returned correction"})
	require.NoError(t, err)
	next := workflowTestSubmission(t, s, owner, a.ID)
	_, err = s.EditRemark(ctx, owner, edited.ID, RemarkInput{Version: edited.Version, Remark: "old"})
	workflowTestStatus(t, err, 403)
	_, err = s.EditRemark(ctx, admin, next.ID, RemarkInput{AccountType: AccountTypeOpening, Version: next.Version, Remark: "cross-pool"})
	workflowTestStatus(t, err, 404)
}
