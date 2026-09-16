package controller

import (
	"context"
	"fmt"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/stretchr/testify/require"
)

func TestMailboxHTTPRemarkOnlySubmissionAndIsolation(t *testing.T) {
	r, s, admin := mailboxControllerFixture(t)
	cookie, csrf, op := mailboxTestLogin(t, r, s, admin, "remark-owner")
	otherCookie, otherCSRF, _ := mailboxTestLogin(t, r, s, admin, "remark-other")
	a := model.MailboxAccount{Email: "remark@example.test", AccountType: "opening", Ciphertext: "synthetic", Version: 1}
	require.NoError(t, s.DB.Create(&a).Error)
	require.NoError(t, s.Assign(context.Background(), admin, mailbox.AssignInput{AccountType: "opening", Items: []mailbox.AssignItem{{ID: a.ID, Version: a.Version}}, OperatorID: op}))
	var assignment model.MailboxAssignment
	require.NoError(t, s.DB.Where("account_id = ?", a.ID).First(&assignment).Error)
	path := fmt.Sprintf("/mailbox-api/v1/assignments/%d/submit?account_type=opening", assignment.ID)
	body := fmt.Sprintf(`{"version":%d,"attachment_ids":[],"remark":"开号完成\n待管理员核对"}`, assignment.Version)
	require.Equal(t, 403, mailboxRequest(r, "POST", path, body, cookie, "").Code)
	require.Equal(t, 404, mailboxRequest(r, "POST", path, body, otherCookie, otherCSRF).Code)
	w := mailboxRequest(r, "POST", path, body, cookie, csrf)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"remark":"开号完成\n待管理员核对"`)
	require.Contains(t, w.Body.String(), `"attachments":[]`)
	w = mailboxRequest(r, "GET", "/api/mailbox-management/submissions?account_type=opening", "", nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "开号完成")
	w = mailboxRequest(r, "GET", "/mailbox-api/v1/submissions?account_type=opening", "", otherCookie, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "开号完成")
	require.Equal(t, 409, mailboxRequest(r, "POST", path, body, cookie, csrf).Code)
}
