package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/middleware"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func mailboxControllerFixture(t *testing.T) (*gin.Engine, *mailbox.Service, mailbox.Actor) {
	t.Helper()
	t.Setenv("MANAGED_INSTANCE_SECRET_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	t.Setenv("MAILBOX_ATTACHMENT_DIR", filepath.Join(t.TempDir(), "private-images"))
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "mailbox.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AdminDataPolicy{}, &model.MailboxAccount{}, &model.MailboxOperator{}, &model.MailboxSession{}, &model.MailboxLoginAttempt{}, &model.MailboxAssignment{}, &model.MailboxSubmission{}, &model.MailboxAttachment{}, &model.MailboxAudit{}))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.AutoMigrate(&model.MailboxIssue{}))
	require.NoError(t, db.AutoMigrate(&model.MailboxCVV{}, &model.MailboxCardIndex{}, &model.MailboxCardIndexState{}))
	old := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = old; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	access, err := authz.LoadDataAccess(db, 1)
	require.NoError(t, err)
	actor := mailbox.Actor{Admin: access, IP: "192.0.2.1"}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api/mailbox-management", MailboxAuditTrail(), func(c *gin.Context) {
		c.Request = c.Request.WithContext(authz.WithDataAccess(c.Request.Context(), access))
		c.Next()
	}, MailboxAdminOriginGuard)
	admin.GET("/accounts", MailboxGuard(authz.MailboxView), ListMailboxAccounts)
	admin.POST("/accounts/archive", MailboxGuard(authz.MailboxManage), ArchiveMailboxAccounts(false))
	admin.POST("/accounts/restore", MailboxGuard(authz.MailboxManage), ArchiveMailboxAccounts(true))
	admin.GET("/accounts/:id", MailboxGuard(authz.MailboxView), GetMailboxAccount)
	admin.POST("/accounts/:id/credentials", MailboxGuard(authz.MailboxCredentials), GetMailboxCredentials)
	admin.POST("/assignments", MailboxGuard(authz.MailboxAssign), AssignMailboxAccounts)
	admin.GET("/assignment-repairs", MailboxGuard(authz.MailboxView), MailboxGuard(authz.MailboxAssign), MailboxGuard(authz.MailboxReview), ListMailboxAssignmentRepairs)
	admin.POST("/assignment-repairs", MailboxGuard(authz.MailboxView), MailboxGuard(authz.MailboxAssign), MailboxGuard(authz.MailboxReview), RepairMailboxAssignments)
	admin.GET("/audits", MailboxGuard(authz.MailboxAudit), ListMailboxAudits)
	admin.GET("/submissions", MailboxGuard(authz.MailboxReview), ListMailboxSubmissions)
	admin.GET("/issues", MailboxGuard(authz.MailboxReview), ListMailboxIssues)
	admin.GET("/issues/:id", MailboxGuard(authz.MailboxReview), GetMailboxIssue)
	admin.POST("/issues/:id/resolve", MailboxGuard(authz.MailboxReview), ResolveMailboxIssue)
	admin.GET("/import-options", MailboxGuard(authz.MailboxManage), GetMailboxImportOptions)
	admin.GET("/operators", MailboxGuard(authz.MailboxOperators), ListMailboxOperators)
	admin.GET("/operators/:id/work-summary", MailboxGuard(authz.MailboxOperators), MailboxGuard(authz.MailboxView), MailboxGuard(authz.MailboxReview), GetMailboxOperatorWorkSummary)
	admin.GET("/operators/:id/work-accounts", MailboxGuard(authz.MailboxOperators), MailboxGuard(authz.MailboxView), MailboxGuard(authz.MailboxReview), ListMailboxOperatorWorkAccounts)
	admin.GET("/operators/:id/work-accounts/:account_id/history", MailboxGuard(authz.MailboxOperators), MailboxGuard(authz.MailboxView), MailboxGuard(authz.MailboxReview), GetMailboxOperatorAccountHistory)
	admin.POST("/operators", MailboxGuard(authz.MailboxOperators), SaveMailboxOperator)
	admin.POST("/imports/preview", MailboxGuard(authz.MailboxManage), ImportMailboxAccounts(true))
	admin.POST("/imports", MailboxGuard(authz.MailboxManage), ImportMailboxAccounts(false))
	admin.POST("/submissions/:id/review", MailboxGuard(authz.MailboxReview), ReviewMailboxSubmission)
	admin.GET("/attachments/:id", MailboxAttachmentGuard, ReadMailboxAttachment)
	portal := r.Group("/mailbox-api/v1", MailboxAuditTrail())
	portal.POST("/auth/login", LoginMailboxPortal)
	portal.GET("/auth/session", GetMailboxPortalSession)
	secured := portal.Group("", MailboxPortalGuard)
	secured.POST("/auth/logout", LogoutMailboxPortal)
	secured.POST("/auth/password", ChangeMailboxPortalPassword)
	secured.GET("/accounts", ListMailboxAccounts)
	secured.GET("/accounts/:id", GetMailboxAccount)
	secured.POST("/accounts/:id/credentials", GetMailboxCredentials)
	secured.GET("/submissions", ListMailboxSubmissions)
	secured.GET("/issues", ListMailboxIssues)
	secured.GET("/issues/:id", GetMailboxIssue)
	secured.POST("/assignments/:id/issues", SubmitMailboxIssue)
	secured.POST("/assignments/:id/attachments", UploadMailboxAttachment)
	secured.POST("/assignments/:id/submit", SubmitMailboxScreenshots)
	secured.GET("/attachments/:id", ReadMailboxAttachment)
	return r, mailboxService(), actor
}

func TestMailboxHTTPOperatorFormContract(t *testing.T) {
	r, _, _ := mailboxControllerFixture(t)
	for _, username := range []string{"x", "ab", "张三", "test user", "_operator", strings.Repeat("a", 97)} {
		body, err := json.Marshal(mailbox.OperatorInput{Username: username, DisplayName: "测试操作员", Password: "test-password-42", Enabled: true})
		require.NoError(t, err)
		w := mailboxRequest(r, "POST", "/api/mailbox-management/operators", string(body), nil, "")
		require.Equal(t, 400, w.Code)
		require.Contains(t, w.Body.String(), "mailbox_invalid_operator")
		require.NotContains(t, w.Body.String(), "test-password-42")
	}
	w := mailboxRequest(r, "POST", "/api/mailbox-management/operators", `{"username":"  Valid.User  ","display_name":"测试操作员","password":"","enabled":true,"version":0}`, nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"username":"valid.user"`)
	require.Contains(t, w.Body.String(), `"generated_password"`)
	w = mailboxRequest(r, "GET", "/api/mailbox-management/operators?page=1&page_size=20&search=", "", nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"total":1`)
	require.NotContains(t, w.Body.String(), "generated_password")
	require.NotContains(t, w.Body.String(), "password_hash")
}

func TestMailboxHTTPFeedbackPausesAndRequiresCurrentAssignment(t *testing.T) {
	r, s, admin := mailboxControllerFixture(t)
	ctx := context.Background()
	cookie, csrf, operatorID := mailboxTestLogin(t, r, s, admin, "issue-owner")
	otherCookie, otherCSRF, _ := mailboxTestLogin(t, r, s, admin, "issue-other")
	a := model.MailboxAccount{Email: "issue-http@example.test", Version: 1, Ciphertext: "synthetic", KeyVersion: "test"}
	require.NoError(t, s.DB.Create(&a).Error)
	require.NoError(t, s.Assign(ctx, admin, mailbox.AssignInput{Items: []mailbox.AssignItem{{ID: a.ID, Version: 1}}, OperatorID: operatorID}))
	v, err := s.GetAccount(ctx, admin, a.ID)
	require.NoError(t, err)
	endpoint := fmt.Sprintf("/mailbox-api/v1/assignments/%d/issues", v.AssignmentID)
	body := fmt.Sprintf(`{"version":%d,"kind":"email_login","description":"Cannot log in","attachment_ids":[]}`, v.AssignmentVersion)
	w := mailboxRequest(r, "POST", endpoint, body, cookie, "")
	require.Equal(t, 403, w.Code)
	w = mailboxRequest(r, "POST", endpoint, body, otherCookie, otherCSRF)
	require.Equal(t, 404, w.Code)
	w = mailboxRequest(r, "POST", endpoint, body, cookie, csrf)
	require.Equal(t, 200, w.Code, w.Body.String())
	var response struct {
		Data mailbox.IssueView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, v.AssignmentVersion, response.Data.SubmittedVersion)
	require.Empty(t, response.Data.Attachments)
	w = mailboxRequest(r, "POST", endpoint, body, cookie, csrf)
	require.Equal(t, 409, w.Code)
	w = mailboxRequest(r, "POST", fmt.Sprintf("/mailbox-api/v1/accounts/%d/credentials", a.ID), `{"kind":"password"}`, cookie, csrf)
	require.Equal(t, 403, w.Code)
	require.Contains(t, w.Body.String(), "mailbox_credentials_revoked")
	w = mailboxRequest(r, "GET", fmt.Sprintf("/mailbox-api/v1/issues/%d", response.Data.ID), "", otherCookie, otherCSRF)
	require.Equal(t, 404, w.Code)
	w = mailboxRequest(r, "GET", fmt.Sprintf("/mailbox-api/v1/issues?assignment_id=%d", v.AssignmentID), "", cookie, csrf)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"total":1`)
	require.NotContains(t, w.Body.String(), "ciphertext")
}

func mailboxRequest(r *gin.Engine, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "https://mailbox.example"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://mailbox.example")
	req.Header.Set("X-Mailbox-Request", "1")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-Mailbox-CSRF", csrf)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func mailboxTestLogin(t *testing.T, r *gin.Engine, s *mailbox.Service, actor mailbox.Actor, username string) (*http.Cookie, string, int64) {
	t.Helper()
	op, err := s.SaveOperator(context.Background(), actor, 0, mailbox.OperatorInput{Username: username, DisplayName: "Synthetic operator", Password: "test-password-42", Enabled: true})
	require.NoError(t, err)
	w := mailboxRequest(r, "POST", "/mailbox-api/v1/auth/login", fmt.Sprintf(`{"username":%q,"password":"test-password-42"}`, username), nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var response struct {
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, w.Result().Cookies(), 1)
	return w.Result().Cookies()[0], response.Data.CSRF, op.ID
}

func TestMailboxHTTPIndependentSessionAndCSRF(t *testing.T) {
	r, s, admin := mailboxControllerFixture(t)
	for _, name := range []string{"session", "supplier_session"} {
		w := mailboxRequest(r, "GET", "/mailbox-api/v1/accounts", "", &http.Cookie{Name: name, Value: "other-session"}, "")
		require.Equal(t, 401, w.Code)
	}
	cookie, csrf, operatorID := mailboxTestLogin(t, r, s, admin, "test-operator")
	require.Equal(t, "mailbox_session", cookie.Name)
	require.Equal(t, "/mailbox-api/v1", cookie.Path)
	require.True(t, cookie.Secure)
	require.True(t, cookie.HttpOnly)
	require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	require.Equal(t, 43200, cookie.MaxAge)
	require.Len(t, csrf, 64)
	require.Equal(t, 403, mailboxRequest(r, "POST", "/mailbox-api/v1/auth/logout", `{}`, cookie, "").Code)
	require.Equal(t, 200, mailboxRequest(r, "GET", "/mailbox-api/v1/accounts", "", cookie, "").Code)
	require.NoError(t, s.RevokeSessions(context.Background(), admin, operatorID))
	require.Equal(t, 401, mailboxRequest(r, "GET", "/mailbox-api/v1/accounts", "", cookie, "").Code)
	var audits []model.MailboxAudit
	require.NoError(t, s.DB.Find(&audits).Error)
	data, err := json.Marshal(audits)
	require.NoError(t, err)
	require.NotContains(t, string(data), cookie.Value)
	require.NotContains(t, string(data), "test-password-42")
}

func TestMailboxHTTPImportCredentialsScreenshotsAndApproval(t *testing.T) {
	r, s, admin := mailboxControllerFixture(t)
	cookie, csrf, operatorID := mailboxTestLogin(t, r, s, admin, "first-operator")
	otherCookie, otherCSRF, otherID := mailboxTestLogin(t, r, s, admin, "second-operator")
	const importBody = `{"format":"text","text":"synthetic@example.com\t password-preserved \tJBSWY3DPEHPK3PXP"}`
	w := mailboxRequest(r, "POST", "/api/mailbox-management/imports/preview", importBody, nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "password-preserved")
	require.NotContains(t, w.Body.String(), "JBSWY3DPEHPK3PXP")
	w = mailboxRequest(r, "POST", "/api/mailbox-management/imports", importBody, nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var account model.MailboxAccount
	require.NoError(t, s.DB.First(&account).Error)
	assignJSON := fmt.Sprintf(`{"items":[{"id":%d,"version":%d}],"operator_id":%d}`, account.ID, account.Version, operatorID)
	var assign mailbox.AssignInput
	require.NoError(t, json.Unmarshal([]byte(assignJSON), &assign))
	require.NoError(t, s.Assign(context.Background(), admin, assign))
	require.NoError(t, s.DB.First(&account, account.ID).Error)
	var assignment model.MailboxAssignment
	require.NoError(t, s.DB.First(&assignment, account.ActiveAssignmentID).Error)
	path := fmt.Sprintf("/mailbox-api/v1/accounts/%d/credentials", account.ID)
	w = mailboxRequest(r, "POST", path, `{"kind":"password"}`, cookie, csrf)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"password":" password-preserved "`)
	require.Contains(t, w.Header().Get("Cache-Control"), "no-store")
	require.Equal(t, 404, mailboxRequest(r, "POST", path, `{"kind":"password"}`, otherCookie, otherCSRF).Code)
	w = mailboxRequest(r, "POST", path, `{"kind":"otp"}`, cookie, csrf)
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), "JBSWY3DPEHPK3PXP")
	require.NotContains(t, w.Body.String(), "password-preserved")
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "private-email.png")
	require.NoError(t, err)
	require.NoError(t, png.Encode(file, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	require.NoError(t, form.Close())
	req := httptest.NewRequest("POST", fmt.Sprintf("https://mailbox.example/mailbox-api/v1/assignments/%d/attachments", assignment.ID), &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("X-Mailbox-CSRF", csrf)
	req.Header.Set("Origin", "https://mailbox.example")
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "storage_key")
	require.NotContains(t, w.Body.String(), "private-email.png")
	var attachment struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &attachment))
	attachmentPath := "/mailbox-api/v1/attachments/" + attachment.Data.ID
	require.Equal(t, 404, mailboxRequest(r, "GET", attachmentPath, "", otherCookie, "").Code)
	w = mailboxRequest(r, "POST", fmt.Sprintf("/mailbox-api/v1/assignments/%d/submit", assignment.ID), fmt.Sprintf(`{"version":%d,"attachment_ids":[%q]}`, assignment.Version, attachment.Data.ID), cookie, csrf)
	require.Equal(t, 200, w.Code, w.Body.String())
	var submission model.MailboxSubmission
	require.NoError(t, s.DB.First(&submission).Error)
	w = mailboxRequest(r, "POST", fmt.Sprintf("/api/mailbox-management/submissions/%d/review", submission.ID), fmt.Sprintf(`{"version":%d,"status":"approved","reason":""}`, submission.Version), nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 403, mailboxRequest(r, "POST", path, `{"kind":"password"}`, cookie, csrf).Code)
	require.Equal(t, 403, mailboxRequest(r, "POST", path, `{"kind":"otp"}`, cookie, csrf).Code)
	require.Equal(t, 200, mailboxRequest(r, "GET", attachmentPath, "", cookie, "").Code)
	require.NoError(t, s.DB.First(&account, account.ID).Error)
	require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf(`{"items":[{"id":%d,"version":%d}],"operator_id":%d}`, account.ID, account.Version, otherID)), &assign))
	status, code := mailbox.HTTPError(s.Assign(context.Background(), admin, assign))
	require.Equal(t, 409, status)
	require.Equal(t, "mailbox_already_submitted", code)
	require.Equal(t, 404, mailboxRequest(r, "GET", attachmentPath, "", otherCookie, "").Code)
	require.Equal(t, 200, mailboxRequest(r, "GET", attachmentPath, "", cookie, "").Code)
	require.Equal(t, 403, mailboxRequest(r, "POST", path, `{"kind":"password"}`, cookie, csrf).Code)
	w = mailboxRequest(r, "GET", "/mailbox-api/v1/submissions", "", otherCookie, "")
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), attachment.Data.ID)
}

func TestMailboxHTTPRejectsCrossOriginMutations(t *testing.T) {
	r, s, admin := mailboxControllerFixture(t)
	cookie, csrf, _ := mailboxTestLogin(t, r, s, admin, "csrf-operator")
	for _, tc := range []struct{ path, origin, site, marker string }{
		{"/api/mailbox-management/imports/preview", "https://mailbox.example", "same-origin", ""},
		{"/api/mailbox-management/imports/preview", "https://sibling.example", "same-site", "1"},
		{"/api/mailbox-management/imports/preview", "http://mailbox.example", "", "1"},
		{"/mailbox-api/v1/auth/logout", "https://sibling.example", "same-site", "1"},
		{"/mailbox-api/v1/auth/logout", "http://mailbox.example", "", "1"},
		{"/mailbox-api/v1/auth/logout", "", "", ""},
	} {
		req := httptest.NewRequest("POST", "https://mailbox.example"+tc.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Sec-Fetch-Site", tc.site)
		req.Header.Set("X-Mailbox-Request", tc.marker)
		req.Header.Set("X-Mailbox-CSRF", csrf)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, 403, w.Code, tc.path+" "+tc.origin)
	}
	require.Equal(t, 200, mailboxRequest(r, "POST", "/mailbox-api/v1/auth/logout", `{}`, cookie, csrf).Code)
}

func TestMailboxHTTPUsesRealAdminAuthorization(t *testing.T) {
	_, s, _ := mailboxControllerFixture(t)
	r := gin.New()
	r.Use(sessions.Sessions("session", cookie.NewStore([]byte("synthetic-test-cookie-key-32-byte"))))
	r.GET("/fixture-login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", "root")
		session.Set("id", 1)
		session.Set("role", common.RoleRootUser)
		session.Set("status", common.UserStatusEnabled)
		session.Set("authorization_version", int64(1))
		require.NoError(t, session.Save())
		c.Status(204)
	})
	r.GET("/api/mailbox-management/accounts", MailboxAuditTrail(), middleware.AdminAuth(), MailboxGuard(authz.MailboxView), ListMailboxAccounts)
	r.GET("/mailbox-api/v1/accounts", MailboxAuditTrail(), MailboxPortalGuard, ListMailboxAccounts)
	login := mailboxRequest(r, "GET", "/fixture-login", "", nil, "")
	require.Equal(t, 204, login.Code)
	consoleCookie := login.Result().Cookies()[0]
	require.Equal(t, 200, mailboxRequest(r, "GET", "/api/mailbox-management/accounts", "", consoleCookie, "").Code)
	require.Equal(t, 401, mailboxRequest(r, "GET", "/mailbox-api/v1/accounts", "", consoleCookie, "").Code)
	require.NoError(t, s.DB.Model(&model.User{}).Where("id = 1").Update("authorization_version", 2).Error)
	w := mailboxRequest(r, "GET", "/api/mailbox-management/accounts", "", consoleCookie, "")
	require.Equal(t, 401, w.Code)
	require.Contains(t, w.Body.String(), "admin_authorization_changed")
}
