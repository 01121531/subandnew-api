package mailbox

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestSubmissionRemarkForBothPoolsAndAllEvidenceModes(t *testing.T) {
	for _, pool := range []string{"refund", "opening"} {
		for _, mode := range []string{"remark", "screenshot", "both"} {
			t.Run(pool+"/"+mode, func(t *testing.T) {
				s, admin, owner, other := workflowTestService(t)
				var err error
				s, err = s.withAccountType(pool)
				require.NoError(t, err)
				a := model.MailboxAccount{Email: "test@example.test", AccountType: pool, Ciphertext: "synthetic", Version: 1}
				require.NoError(t, s.DB.Create(&a).Error)
				require.NoError(t, s.Assign(t.Context(), admin, AssignInput{AccountType: pool, Items: []AssignItem{{ID: a.ID, Version: a.Version}}, OperatorID: owner.OperatorID}))
				view, err := s.GetAccount(t.Context(), owner, a.ID, pool)
				require.NoError(t, err)
				ids := []string{}
				if mode != "remark" {
					file, err := s.UploadAttachment(t.Context(), owner, view.AssignmentID, bytes.NewReader(mailboxTestPNG(t)), pool)
					require.NoError(t, err)
					ids = append(ids, file.ID)
				}
				remark := ""
				if mode != "screenshot" {
					remark = "  已完成处理\n第二行 <script>plain text</script>  "
				}
				result, err := s.SubmitWithRemark(t.Context(), owner, view.AssignmentID, view.AssignmentVersion, ids, remark, pool)
				require.NoError(t, err)
				require.Equal(t, strings.TrimSpace(remark), result.Remark)
				require.Len(t, result.Attachments, len(ids))
				require.Equal(t, StatusPending, result.Status)
				current, err := s.GetAccount(t.Context(), owner, a.ID, pool)
				require.NoError(t, err)
				require.Equal(t, StatusSubmitted, current.Status)
				for _, actor := range []Actor{admin, owner} {
					history, err := s.ListSubmissions(t.Context(), actor, ListQuery{AccountType: pool})
					require.NoError(t, err)
					require.Len(t, history.Items, 1)
					require.Equal(t, result.Remark, history.Items[0].Remark)
				}
				history, err := s.ListSubmissions(t.Context(), other, ListQuery{AccountType: pool})
				require.NoError(t, err)
				require.Empty(t, history.Items)
				var audits []model.MailboxAudit
				require.NoError(t, s.DB.Find(&audits).Error)
				raw, err := json.Marshal(audits)
				require.NoError(t, err)
				require.NotContains(t, string(raw), "第二行")
				_, err = s.SubmitWithRemark(t.Context(), owner, view.AssignmentID, view.AssignmentVersion, ids, "retry", pool)
				workflowTestStatus(t, err, 409)
			})
		}
	}
}

func TestSubmissionRemarkValidationAndReviewHistory(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	a := workflowTestAccount(t, s, "remark@example.test")
	v := workflowTestAssign(t, s, admin, owner, a.ID)
	for _, remark := range []string{"", " \r\n\t ", strings.Repeat("中", 2001), "control\x00char", string([]byte{0xff})} {
		_, err := s.SubmitWithRemark(t.Context(), owner, v.AssignmentID, v.AssignmentVersion, nil, remark)
		workflowTestStatus(t, err, 400)
	}
	_, err := s.SubmitWithRemark(t.Context(), other, v.AssignmentID, v.AssignmentVersion, nil, "other's note")
	workflowTestStatus(t, err, 404)
	_, err = s.SubmitWithRemark(t.Context(), admin, v.AssignmentID, v.AssignmentVersion, nil, "admin cannot submit")
	workflowTestStatus(t, err, 403)
	_, err = s.SubmitWithRemark(t.Context(), owner, v.AssignmentID, v.AssignmentVersion, []string{"invalid-file"}, "valid note")
	workflowTestStatus(t, err, 400)
	first, err := s.SubmitWithRemark(t.Context(), owner, v.AssignmentID, v.AssignmentVersion, nil, strings.Repeat("😀", 2000))
	require.NoError(t, err)
	require.NoError(t, s.Review(t.Context(), admin, first.ID, ReviewInput{Version: first.Version, Status: StatusRejected, Reason: "请补充说明"}))
	v, err = s.GetAccount(t.Context(), owner, a.ID)
	require.NoError(t, err)
	second, err := s.SubmitWithRemark(t.Context(), owner, v.AssignmentID, v.AssignmentVersion, nil, "补充备注")
	require.NoError(t, err)
	require.NoError(t, s.Review(t.Context(), admin, second.ID, ReviewInput{Version: second.Version, Status: StatusApproved}))
	history, err := s.ListSubmissions(t.Context(), owner, ListQuery{})
	require.NoError(t, err)
	require.Len(t, history.Items, 2)
	require.Equal(t, "补充备注", history.Items[0].Remark)
	require.Equal(t, strings.Repeat("😀", 2000), history.Items[1].Remark)
	v, err = s.GetAccount(t.Context(), owner, a.ID)
	require.NoError(t, err)
	require.False(t, v.CredentialsAvailable)
	_, err = s.SubmitWithRemark(t.Context(), owner, v.AssignmentID, v.AssignmentVersion, nil, "cannot change approved")
	workflowTestStatus(t, err, 409)
}

func TestConcurrentRemarkSubmissionsDoNotDuplicate(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	a := workflowTestAccount(t, s, "concurrent-note@example.test")
	v := workflowTestAssign(t, s, admin, owner, a.ID)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.SubmitWithRemark(context.Background(), owner, v.AssignmentID, v.AssignmentVersion, nil, "one submission")
			results <- err
		}()
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
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxSubmission{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
