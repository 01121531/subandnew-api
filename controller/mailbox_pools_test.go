package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestMailboxHTTPPoolsAndCardCredentials(t *testing.T) {
	r, s, admin := mailboxControllerFixture(t)
	cookie, csrf, operatorID := mailboxTestLogin(t, r, s, admin, "pool-operator")
	otherCookie, otherCSRF, _ := mailboxTestLogin(t, r, s, admin, "pool-other")
	const refund = `{"format":"text","text":"same@example.test----refund-secret----JBSWY3DPEHPK3PXP"}`
	const opening = `{"account_type":"opening","format":"text","text":"same@example.test----opening-secret----JBSWY3DPEHPK3PXP----4242 4242 4242 4242----12/99"}`
	for _, body := range []string{refund, opening} {
		w := mailboxRequest(r, "POST", "/api/mailbox-management/imports/preview", body, nil, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"valid":true`)
		require.NotContains(t, w.Body.String(), "secret")
		require.NotContains(t, w.Body.String(), "4242424242424242")
		require.NotContains(t, w.Body.String(), "4242 4242 4242 4242")
		w = mailboxRequest(r, "POST", "/api/mailbox-management/imports", body, nil, "")
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	var accounts []model.MailboxAccount
	require.NoError(t, s.DB.Order("id").Find(&accounts).Error)
	require.Len(t, accounts, 2)
	mixed := fmt.Sprintf(`{"account_type":"refund","operator_id":%d,"items":[{"id":%d,"version":%d},{"id":%d,"version":%d}]}`, operatorID, accounts[0].ID, accounts[0].Version, accounts[1].ID, accounts[1].Version)
	mixedResult := mailboxRequest(r, "POST", "/api/mailbox-management/assignments", mixed, nil, "")
	require.NotEqual(t, 200, mixedResult.Code)
	var assignments int64
	require.NoError(t, s.DB.Model(&model.MailboxAssignment{}).Count(&assignments).Error)
	require.Zero(t, assignments)
	for _, account := range accounts {
		body := fmt.Sprintf(`{"account_type":%q,"operator_id":%d,"items":[{"id":%d,"version":%d}]}`, account.AccountType, operatorID, account.ID, account.Version)
		w := mailboxRequest(r, "POST", "/api/mailbox-management/assignments", body, nil, "")
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	for _, scope := range []string{"refund", "opening"} {
		for _, prefix := range []string{"/api/mailbox-management", "/mailbox-api/v1"} {
			w := mailboxRequest(r, "GET", prefix+"/accounts?account_type="+scope, "", cookie, csrf)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"total":1`)
			require.Contains(t, w.Body.String(), `"account_type":"`+scope+`"`)
			require.NotContains(t, w.Body.String(), "4242424242424242")
			require.NotContains(t, w.Body.String(), "12/99")
			require.NotContains(t, w.Body.String(), "ciphertext")
		}
	}
	w := mailboxRequest(r, "GET", "/mailbox-api/v1/accounts", "", cookie, "")
	require.Contains(t, w.Body.String(), `"account_type":"refund"`)
	require.NotContains(t, w.Body.String(), `"account_type":"opening"`)
	wrongScope := fmt.Sprintf("/mailbox-api/v1/accounts/%d?account_type=refund", accounts[1].ID)
	require.Equal(t, 404, mailboxRequest(r, "GET", wrongScope, "", cookie, "").Code)
	legacyCard := fmt.Sprintf("/mailbox-api/v1/accounts/%d/credentials", accounts[1].ID)
	require.Equal(t, 404, mailboxRequest(r, "POST", legacyCard, `{"kind":"card"}`, cookie, csrf).Code)
	path := fmt.Sprintf("/mailbox-api/v1/accounts/%d/credentials?account_type=opening", accounts[1].ID)
	w = mailboxRequest(r, "POST", path, `{"kind":"card"}`, cookie, csrf)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"card_number":"4242424242424242"`)
	require.Contains(t, w.Body.String(), `"card_expiry":"12/99"`)
	require.NotContains(t, w.Body.String(), "cvv")
	require.NotContains(t, w.Body.String(), "opening-secret")
	require.Contains(t, w.Header().Get("Cache-Control"), "no-store")
	require.Equal(t, 404, mailboxRequest(r, "POST", path, `{"kind":"card"}`, otherCookie, otherCSRF).Code)
	t.Setenv("MAILBOX_TEMP_CVV_MODE", "")
	require.Equal(t, 404, mailboxRequest(r, "POST", path, `{"kind":"cvv"}`, cookie, csrf).Code)
	refundPath := fmt.Sprintf("/mailbox-api/v1/accounts/%d/credentials", accounts[0].ID)
	require.Equal(t, 400, mailboxRequest(r, "POST", refundPath, `{"kind":"card"}`, cookie, csrf).Code)
	var active model.MailboxAccount
	require.NoError(t, s.DB.First(&active, accounts[1].ID).Error)
	body := fmt.Sprintf(`{"account_type":"opening","operator_id":0,"items":[{"id":%d,"version":%d}]}`, active.ID, active.Version)
	require.Equal(t, 200, mailboxRequest(r, "POST", "/api/mailbox-management/assignments", body, nil, "").Code)
	require.NotEqual(t, 200, mailboxRequest(r, "POST", path, `{"kind":"card"}`, cookie, csrf).Code)
	var audits []model.MailboxAudit
	require.NoError(t, s.DB.Find(&audits).Error)
	encoded, err := json.Marshal(audits)
	require.NoError(t, err)
	for _, secret := range []string{"4242424242424242", "12/99", "opening-secret", "refund-secret", "JBSWY3DPEHPK3PXP"} {
		require.NotContains(t, string(encoded), secret)
	}
	for _, scope := range []string{"refund", "opening"} {
		w = mailboxRequest(r, "GET", "/api/mailbox-management/audits?account_type="+scope, "", nil, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var response struct {
			Data struct {
				Items []model.MailboxAudit `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.NotEmpty(t, response.Data.Items)
		for _, event := range response.Data.Items {
			require.Equal(t, scope, event.AccountType)
		}
	}
}

func TestMailboxHTTPPoolImportValidation(t *testing.T) {
	r, _, _ := mailboxControllerFixture(t)
	for _, path := range []string{"accounts", "audits", "submissions", "operators"} {
		require.Equal(t, 400, mailboxRequest(r, "GET", "/api/mailbox-management/"+path+"?account_type=invalid", "", nil, "").Code)
	}
	for _, body := range []string{
		`{"account_type":"invalid","format":"text","text":""}`,
		`{"account_type":"opening","format":"text","text":"","cvv":"123"}`,
	} {
		w := mailboxRequest(r, "POST", "/api/mailbox-management/imports/preview", body, nil, "")
		require.Equal(t, 400, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "123")
	}
	for _, extra := range []bool{false, true} {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		require.NoError(t, form.WriteField("account_type", "opening"))
		require.NoError(t, form.WriteField("format", "csv"))
		if extra {
			require.NoError(t, form.WriteField("cvv", "123"))
		}
		file, err := form.CreateFormFile("file", "example.csv")
		require.NoError(t, err)
		_, err = file.Write([]byte("email,password,2fa,card_number,card_expiry\nfile@example.test,test-password,JBSWY3DPEHPK3PXP,4242424242424242,12/99"))
		require.NoError(t, err)
		require.NoError(t, form.Close())
		req := httptest.NewRequest("POST", "https://mailbox.example/api/mailbox-management/imports/preview", &body)
		req.Header.Set("Content-Type", form.FormDataContentType())
		req.Header.Set("Origin", "https://mailbox.example")
		req.Header.Set("X-Mailbox-Request", "1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if extra {
			require.Equal(t, 400, w.Code, w.Body.String())
		} else {
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"valid":true`)
		}
		require.NotContains(t, w.Body.String(), "4242424242424242")
	}
}
