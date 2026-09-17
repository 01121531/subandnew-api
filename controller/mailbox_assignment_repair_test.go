package controller

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMailboxAssignmentRepairHTTP(t *testing.T) {
	r, s, _ := mailboxControllerFixture(t)
	op := model.MailboxOperator{Username: "repair-owner", DisplayName: "Original", PasswordHash: "private-hash", Enabled: true, Version: 1, AuthVersion: 1}
	require.NoError(t, s.DB.Create(&op).Error)
	a := model.MailboxAccount{AccountType: "refund", Email: "repair@example.test", Ciphertext: "private-cipher", Version: 1}
	require.NoError(t, s.DB.Create(&a).Error)
	original := model.MailboxAssignment{AccountID: a.ID, OperatorID: op.ID, Status: mailbox.StatusSubmitted, Version: 2, CreatedAt: 100, RevokedAt: 300}
	require.NoError(t, s.DB.Create(&original).Error)
	sub := model.MailboxSubmission{AssignmentID: original.ID, OperatorID: op.ID, Status: mailbox.StatusPending, Version: 1, CreatedAt: 200, Remark: "keep original remark"}
	require.NoError(t, s.DB.Create(&sub).Error)
	w := mailboxRequest(r, "GET", "/api/mailbox-management/assignment-repairs?account_type=refund", "", nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "private-")
	var response struct {
		Data mailbox.Page[mailbox.RepairCandidate] `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.EqualValues(t, 1, response.Data.Total)
	require.True(t, response.Data.Items[0].CanRepair)
	body, _ := json.Marshal(mailbox.RepairInput{AccountType: "refund", Reason: "Restore accidental recall", Items: []mailbox.RepairItem{response.Data.Items[0].RepairItem}})
	w = mailboxRequest(r, "POST", "/api/mailbox-management/assignment-repairs", string(body), nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	w = mailboxRequest(r, "POST", "/api/mailbox-management/assignment-repairs", string(body), nil, "")
	require.Equal(t, 409, w.Code, w.Body.String())
	require.NoError(t, s.DB.First(&a, a.ID).Error)
	w = mailboxRequest(r, "POST", "/api/mailbox-management/assignments", fmt.Sprintf(`{"account_type":"refund","operator_id":%d,"items":[{"id":%d,"version":%d}]}`, op.ID, a.ID, a.Version), nil, "")
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "mailbox_already_submitted")
	require.Contains(t, w.Body.String(), "repair@example.test")
	for _, q := range []string{"account_type=other", "page=-1", "operator_id=abc"} {
		w = mailboxRequest(r, "GET", "/api/mailbox-management/assignment-repairs?"+q, "", nil, "")
		require.Equal(t, 400, w.Code, w.Body.String())
	}
}

func TestMailboxAssignmentRepairPermissions(t *testing.T) {
	_, s, _ := mailboxControllerFixture(t)
	require.NoError(t, s.DB.AutoMigrate(&model.CasbinRule{}))
	require.NoError(t, authz.Init(s.DB))
	u := model.User{Username: "repair-restricted", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, s.DB.Create(&u).Error)
	access, err := authz.LoadDataAccess(s.DB, u.Id)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: {}}))
	})
	r := gin.New()
	g := r.Group("/api/mailbox-management", func(c *gin.Context) {
		c.Request = c.Request.WithContext(authz.WithDataAccess(c.Request.Context(), access))
		c.Next()
	})
	g.GET("/assignment-repairs", MailboxGuard(authz.MailboxView), MailboxGuard(authz.MailboxAssign), MailboxGuard(authz.MailboxReview), ListMailboxAssignmentRepairs)
	g.POST("/assignment-repairs", MailboxGuard(authz.MailboxView), MailboxGuard(authz.MailboxAssign), MailboxGuard(authz.MailboxReview), RepairMailboxAssignments)
	for _, actions := range []map[string]bool{{"view": true, "assign": true}, {"view": true, "review": true}, {"assign": true, "review": true}} {
		require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: actions}))
		for _, method := range []string{"GET", "POST"} {
			w := mailboxRequest(r, method, "/api/mailbox-management/assignment-repairs", "{}", nil, "")
			require.Equal(t, 403, w.Code, w.Body.String())
		}
	}
	require.NoError(t, authz.SetUserPermissions(u.Id, authz.PermissionsMap{authz.ResourceMailboxManagement: {"view": true, "assign": true, "review": true}}))
	w := mailboxRequest(r, "GET", "/api/mailbox-management/assignment-repairs", "", nil, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, s.DB.Model(&u).Update("authorization_version", access.Version+1).Error)
	w = mailboxRequest(r, "GET", "/api/mailbox-management/assignment-repairs", "", nil, "")
	require.Equal(t, 401, w.Code, w.Body.String())
}
