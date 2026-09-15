package controller

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestMailboxHTTPArchiveAndRestore(t *testing.T) {
	r, s, admin := mailboxControllerFixture(t)
	cookie, csrf, _ := mailboxTestLogin(t, r, s, admin, "archive-operator")
	for _, kind := range []string{"refund", "opening"} {
		a := model.MailboxAccount{AccountType: kind, Email: "archive@example.test", Ciphertext: "private", KeyVersion: "key", Version: 1}
		require.NoError(t, s.DB.Create(&a).Error)
		body := fmt.Sprintf(`{"account_type":%q,"items":[{"id":%d,"version":1}]}`, kind, a.ID)
		w := mailboxRequest(r, "POST", "/api/mailbox-management/accounts/archive", body, nil, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		w = mailboxRequest(r, "GET", "/api/mailbox-management/accounts?account_type="+kind, "", nil, "")
		require.Equal(t, 200, w.Code)
		require.Contains(t, w.Body.String(), `"total":0`)
		w = mailboxRequest(r, "GET", "/api/mailbox-management/accounts?archived=true&account_type="+kind, "", nil, "")
		require.Equal(t, 200, w.Code)
		require.Contains(t, w.Body.String(), `"total":1`)
		require.Contains(t, w.Body.String(), `"credentials_available":false`)
		require.NotContains(t, w.Body.String(), "private")
		path := fmt.Sprintf("/api/mailbox-management/accounts/%d/credentials?account_type=%s", a.ID, kind)
		require.Equal(t, 403, mailboxRequest(r, "POST", path, `{"kind":"password"}`, nil, "").Code)
		require.Equal(t, 403, mailboxRequest(r, "GET", "/mailbox-api/v1/accounts?archived=true&account_type="+kind, "", cookie, csrf).Code)
		require.Equal(t, 404, mailboxRequest(r, "POST", "/mailbox-api/v1/accounts/archive", body, cookie, csrf).Code)
		require.Equal(t, 400, mailboxRequest(r, "GET", "/api/mailbox-management/accounts?archived=invalid", "", nil, "").Code)
		require.Equal(t, 409, mailboxRequest(r, "POST", "/api/mailbox-management/accounts/restore", body, nil, "").Code)
		body = fmt.Sprintf(`{"account_type":%q,"items":[{"id":%d,"version":2}]}`, kind, a.ID)
		require.Equal(t, 200, mailboxRequest(r, "POST", "/api/mailbox-management/accounts/restore", body, nil, "").Code)
		var current model.MailboxAccount
		require.NoError(t, s.DB.First(&current, a.ID).Error)
		require.Zero(t, current.ArchivedAt)
		require.Zero(t, current.ActiveAssignmentID)
	}
	for _, body := range []string{`{}`, `{"items":[]}`, `{"items":[{"id":1,"version":1}],"unexpected":true}`} {
		require.Equal(t, 400, mailboxRequest(r, "POST", "/api/mailbox-management/accounts/archive", body, nil, "").Code)
	}
	var events []model.MailboxAudit
	require.NoError(t, s.DB.Where("action IN ?", []string{"archive", "restore"}).Find(&events).Error)
	require.Len(t, events, 4)
	data, err := json.Marshal(events)
	require.NoError(t, err)
	require.NotContains(t, string(data), "private")
}
