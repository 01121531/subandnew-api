package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMailboxOperatorWorkHTTPCountsAndHistory(t *testing.T) {
	r, s, actor := mailboxControllerFixture(t)
	op, err := s.SaveOperator(context.Background(), actor, 0, mailbox.OperatorInput{Username: "work-owner", DisplayName: "Work owner", Password: "synthetic-password", Enabled: true})
	require.NoError(t, err)
	other, err := s.SaveOperator(context.Background(), actor, 0, mailbox.OperatorInput{Username: "work-other", DisplayName: "Other", Password: "synthetic-password", Enabled: true})
	require.NoError(t, err)
	date := time.Date(2026, 9, 16, 0, 0, 0, 0, time.FixedZone("CST", 8*3600)).Unix()
	a := model.MailboxAccount{AccountType: "opening", Email: "same@example.test", Ciphertext: "do-not-expose-cipher", CardCiphertext: "do-not-expose-card", KeyVersion: "test", CardLast4: "4242", Version: 1}
	require.NoError(t, s.DB.Create(&a).Error)
	task := model.MailboxAssignment{AccountID: a.ID, OperatorID: op.ID, Status: mailbox.StatusSubmitted, Version: 1, CreatedAt: date - 100, UpdatedAt: date + 100}
	require.NoError(t, s.DB.Create(&task).Error)
	require.NoError(t, s.DB.Model(&a).Update("active_assignment_id", task.ID).Error)
	for _, at := range []int64{date - 1, date, date + 100} {
		require.NoError(t, s.DB.Create(&model.MailboxSubmission{AssignmentID: task.ID, OperatorID: op.ID, Status: "pending", Version: 1, CreatedAt: at}).Error)
	}
	foreign := model.MailboxAssignment{AccountID: a.ID, OperatorID: other.ID, Status: mailbox.StatusRejected, Version: 1, RevokedAt: date - 50, CreatedAt: date - 200}
	require.NoError(t, s.DB.Create(&foreign).Error)
	require.NoError(t, s.DB.Create(&model.MailboxSubmission{AssignmentID: foreign.ID, OperatorID: other.ID, Status: "rejected", ReviewReason: "other-operator-private-reason", Version: 1, CreatedAt: date}).Error)
	read := func(path string) map[string]any {
		t.Helper()
		w := mailboxRequest(r, "GET", path, "", nil, "")
		require.Equal(t, 200, w.Code, w.Body.String())
		for _, secret := range []string{"do-not-expose-cipher", "do-not-expose-card", "password_hash", "synthetic-password", "other-operator-private-reason"} {
			require.NotContains(t, w.Body.String(), secret)
		}
		var response struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		return response.Data
	}
	prefix := fmt.Sprintf("/api/mailbox-management/operators/%d", op.ID)
	legacy := read("/api/mailbox-management/operators?search=work-owner")
	legacyItem := legacy["items"].([]any)[0].(map[string]any)
	require.NotContains(t, legacyItem, "work_summary")
	page := read("/api/mailbox-management/operators?include_stats=true&search=work-owner&period=custom&start_date=2026-09-16&end_date=2026-09-16")
	require.EqualValues(t, 1, page["total"])
	summary := page["items"].([]any)[0].(map[string]any)["work_summary"].(map[string]any)
	require.EqualValues(t, 1, summary["submitted_accounts"])
	require.EqualValues(t, 1, summary["opening_submitted"])
	require.EqualValues(t, 0, summary["refund_submitted"])
	require.EqualValues(t, 2, summary["submission_count"])
	overview := read(prefix + "/work-summary?period=custom&start_date=2026-09-16&end_date=2026-09-16")
	require.Equal(t, summary, overview["summary"])
	rangeData := overview["range"].(map[string]any)
	require.EqualValues(t, date, rangeData["start_at"])
	require.EqualValues(t, date+86400, rangeData["end_at"])
	list := read(prefix + "/work-accounts?scope=submitted&account_type=opening&page_size=1")
	require.EqualValues(t, 1, list["total"])
	require.EqualValues(t, a.ID, list["items"].([]any)[0].(map[string]any)["id"])
	history := read(fmt.Sprintf("%s/work-accounts/%d/history?account_type=opening&page_size=1", prefix, a.ID))
	require.EqualValues(t, 3, history["submissions"].(map[string]any)["total"])
	require.Len(t, history["submissions"].(map[string]any)["items"], 1)
	require.EqualValues(t, 1, history["assignments"].(map[string]any)["total"])
	second := read(fmt.Sprintf("%s/work-accounts/%d/history?account_type=opening&page_size=1&submission_page=2", prefix, a.ID))
	require.NotEqual(t, history["submissions"].(map[string]any)["items"], second["submissions"].(map[string]any)["items"])
	w := mailboxRequest(r, "GET", fmt.Sprintf("%s/work-accounts/%d/history?account_type=refund", prefix, a.ID), "", nil, "")
	require.Equal(t, 404, w.Code, w.Body.String())
}

func TestMailboxOperatorWorkHTTPRejectsInvalidQueries(t *testing.T) {
	r, s, actor := mailboxControllerFixture(t)
	op, err := s.SaveOperator(context.Background(), actor, 0, mailbox.OperatorInput{Username: "work-query", DisplayName: "Query", Password: "synthetic-password", Enabled: true})
	require.NoError(t, err)
	prefix := fmt.Sprintf("/api/mailbox-management/operators/%d", op.ID)
	for _, query := range []string{"period=unknown", "period=custom", "period=custom&start_date=2026-02-30&end_date=2026-03-01", "period=custom&start_date=2026-09-17&end_date=2026-09-16", "page=0", "page_size=101", "page=1&page=2", "period=all&period=today", "account_type=invalid", "scope=invalid"} {
		t.Run(query, func(t *testing.T) {
			w := mailboxRequest(r, "GET", prefix+"/work-accounts?"+query, "", nil, "")
			require.Equal(t, 400, w.Code, w.Body.String())
		})
	}
	for _, query := range []string{"include_stats=bogus", "include_stats=true&include_stats=false", "include_stats=true&page=-1"} {
		w := mailboxRequest(r, "GET", "/api/mailbox-management/operators?"+query, "", nil, "")
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	for _, query := range []string{"", "account_type=all", "account_type=opening&issue_page=0", "account_type=opening&submission_page=1&submission_page=2"} {
		w := mailboxRequest(r, "GET", prefix+"/work-accounts/1/history?"+query, "", nil, "")
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	w := mailboxRequest(r, "GET", "/api/mailbox-management/operators/invalid/work-summary", "", nil, "")
	require.Equal(t, 400, w.Code)
}

func TestMailboxOperatorWorkHTTPRequiresAllPermissions(t *testing.T) {
	_, s, root := mailboxControllerFixture(t)
	op, err := s.SaveOperator(context.Background(), root, 0, mailbox.OperatorInput{Username: "permissions-operator", DisplayName: "Operator", Password: "synthetic-password", Enabled: true})
	require.NoError(t, err)
	require.NoError(t, s.DB.AutoMigrate(&model.CasbinRule{}))
	require.NoError(t, authz.Init(s.DB))
	u := model.User{Username: "restricted-work-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, s.DB.Create(&u).Error)
	t.Cleanup(func() {
		require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: {}}))
	})
	access, err := authz.LoadDataAccess(s.DB, u.Id)
	require.NoError(t, err)
	require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: {"operators": true}}))
	r := gin.New()
	g := r.Group("/api/mailbox-management", func(c *gin.Context) {
		c.Request = c.Request.WithContext(authz.WithDataAccess(c.Request.Context(), access))
		c.Next()
	})
	g.GET("/operators", MailboxGuard(authz.MailboxOperators), ListMailboxOperators)
	g.GET("/operators/:id/work-summary", MailboxGuard(authz.MailboxOperators), MailboxGuard(authz.MailboxView), MailboxGuard(authz.MailboxReview), GetMailboxOperatorWorkSummary)
	g.GET("/operators/:id/work-accounts", MailboxGuard(authz.MailboxOperators), MailboxGuard(authz.MailboxView), MailboxGuard(authz.MailboxReview), ListMailboxOperatorWorkAccounts)
	g.GET("/operators/:id/work-accounts/:account_id/history", MailboxGuard(authz.MailboxOperators), MailboxGuard(authz.MailboxView), MailboxGuard(authz.MailboxReview), GetMailboxOperatorAccountHistory)
	w := mailboxRequest(r, "GET", "/api/mailbox-management/operators", "", nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "work_summary")
	for _, actions := range []map[string]bool{{"operators": true}, {"operators": true, "view": true}, {"operators": true, "review": true}, {"view": true, "review": true}} {
		require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: actions}))
		for _, path := range []string{"/api/mailbox-management/operators?include_stats=true", fmt.Sprintf("/api/mailbox-management/operators/%d/work-summary", op.ID), fmt.Sprintf("/api/mailbox-management/operators/%d/work-accounts", op.ID), fmt.Sprintf("/api/mailbox-management/operators/%d/work-accounts/1/history?account_type=opening", op.ID)} {
			w = mailboxRequest(r, "GET", path, "", nil, "")
			require.Equal(t, 403, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "submitted_accounts")
		}
	}
	require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: {"operators": true, "view": true, "review": true}}))
	w = mailboxRequest(r, "GET", "/api/mailbox-management/operators?include_stats=true", "", nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, s.DB.Model(&u).Update("authorization_version", access.Version+1).Error)
	w = mailboxRequest(r, "GET", fmt.Sprintf("/api/mailbox-management/operators/%d/work-summary", op.ID), "", nil, "")
	require.Equal(t, 401, w.Code, w.Body.String())
}
