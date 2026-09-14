package accountdataapi

import (
	"encoding/json"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func delegatedAPI(t *testing.T) (*gorm.DB, *CreateResult, ConfigInput) {
	t.Helper()
	db, instance := setupAPIServiceTest(t)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	master := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = master })
	require.NoError(t, authz.Init(db))
	require.NoError(t, db.Create(&model.User{Id: 2, Username: "responsible-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, authz.SetUserPermissions(2, authz.PermissionsMap{authz.ResourceManagedAccountAPI: {"manage": true}}))
	policy := model.EmptyAdminDataPolicy()
	policy.UserID, policy.InstanceScope, policy.InstanceIDs = 2, "selected", []int64{instance.Id}
	require.NoError(t, db.Create(&policy).Error)
	input := apiInput(instance.Id)
	input.IncludeTerms = nil
	input.Fields = []string{"name", "email", "requests"}
	created, err := Create(t.Context(), input, 7)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.ManagedAccountAPI{}).Where("id = ?", created.API.ID).UpdateColumn("created_by", 2).Error)
	return db, created, input
}

func TestDelegatedAPIProjectsFinalEnvelopeWithoutHiddenZeroFields(t *testing.T) {
	_, created, _ := delegatedAPI(t)
	auth, err := Authenticate(created.Secret, "203.0.113.1")
	require.NoError(t, err)
	require.Equal(t, []string{"name"}, auth.View.Fields)
	result, err := QueryExternal(t.Context(), auth, 1, 50, "", "", "")
	require.NoError(t, err)
	response, err := ProjectResponse(auth.View, map[string]any{"data": result.Items, "pagination": map[string]any{"total": result.Total}, "summary": result.Summary})
	require.NoError(t, err)
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	for _, hidden := range []string{`"email"`, `"requests"`, `"total"`, `"amounts"`, `"tokens"`} {
		require.NotContains(t, string(encoded), hidden)
	}
	_, err = QueryExternal(t.Context(), auth, 1, 50, "secret@example.com", "", "")
	require.Error(t, err)
}

func TestDelegatedAPIOwnerRevocationAndAuditableTakeover(t *testing.T) {
	for _, change := range []string{"disabled", "retired", "manage", "instances"} {
		t.Run(change, func(t *testing.T) {
			db, created, input := delegatedAPI(t)
			auth, err := Authenticate(created.Secret, "203.0.113.1")
			require.NoError(t, err)
			switch change {
			case "disabled":
				require.NoError(t, db.Model(&model.User{}).Where("id = 2").Update("status", common.UserStatusDisabled).Error)
			case "retired":
				require.NoError(t, db.Delete(&model.User{}, 2).Error)
			case "manage":
				require.NoError(t, authz.SetUserPermissions(2, authz.PermissionsMap{authz.ResourceManagedAccountAPI: {"manage": false}}))
			case "instances":
				require.NoError(t, db.Model(&model.AdminDataPolicy{}).Where("user_id = 2").UpdateColumn("instance_ids", "[]").Error)
			}
			_, err = Authenticate(created.Secret, "203.0.113.1")
			require.Error(t, err)
			_, err = QueryExternal(t.Context(), auth, 1, 50, "", "", "")
			require.Error(t, err)
			input.Takeover = true
			view, err := Update(t.Context(), created.API.ID, input, 7)
			require.NoError(t, err)
			require.Equal(t, 7, view.CreatedBy)
			_, err = Authenticate(created.Secret, "203.0.113.1")
			require.ErrorIs(t, err, ErrUnauthorized)
		})
	}
}

func TestDelegatedAPINeverFallsBackFromMissingAssignedOwner(t *testing.T) {
	db, instance := setupAPIServiceTest(t)
	input := apiInput(instance.Id)
	created, err := Create(t.Context(), input, 7)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.ManagedAccountAPI{}).Where("id = ?", created.API.ID).UpdateColumn("created_by", 0).Error)
	_, err = Authenticate(created.Secret, "203.0.113.1")
	require.NoError(t, err)
	require.NoError(t, BackfillLegacyOwners(db))
	require.NoError(t, db.Model(&model.ManagedAccountAPI{}).Where("id = ?", created.API.ID).UpdateColumn("created_by", 999).Error)
	_, err = Authenticate(created.Secret, "203.0.113.1")
	require.ErrorIs(t, err, ErrDisabled)
}

func TestDelegatedAPIRechecksCredentialAndSameSecondConfigAtResponse(t *testing.T) {
	db, instance := setupAPIServiceTest(t)
	created, err := Create(t.Context(), apiInput(instance.Id), 7)
	require.NoError(t, err)
	auth, err := Authenticate(created.Secret, "203.0.113.1")
	require.NoError(t, err)
	result, err := QueryExternal(t.Context(), auth, 1, 50, "", "", "")
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.ManagedAccountAPI{}).Where("id = ?", auth.API.ID).UpdateColumn("fields", `["name"]`).Error)
	_, err = ProjectResponse(auth.View, result)
	require.ErrorIs(t, err, ErrDisabled)
	auth, err = Authenticate(created.Secret, "203.0.113.1")
	require.NoError(t, err)
	require.NoError(t, RevokeKey(auth.API.ID, auth.Key.ID))
	_, err = ProjectResponse(auth.View, result)
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestAccountAPIOwnerBackfillAllowsFreshSetupButNotOrphans(t *testing.T) {
	db, _ := setupAPIServiceTest(t)
	require.NoError(t, db.Delete(&model.User{}, 7).Error)
	require.NoError(t, BackfillLegacyOwners(db))
	require.NoError(t, db.Create(&model.ManagedAccountAPI{Name: "orphan", CreatedBy: 0}).Error)
	require.Error(t, BackfillLegacyOwners(db))
}

func TestTakeoverRecoversWithoutSnapshotsAndPreservesConfiguration(t *testing.T) {
	db, created, _ := delegatedAPI(t)
	require.NoError(t, db.Where("1 = 1").Delete(&model.ManagedAccountSnapshot{}).Error)
	require.NoError(t, db.Delete(&model.User{}, 2).Error)
	var before model.ManagedAccountAPI
	require.NoError(t, db.First(&before, created.API.ID).Error)
	require.NoError(t, db.Create(&model.ManagedAccountAPIPortalSession{APIID: before.ID, TokenHash: "old-session"}).Error)
	_, _, err := Takeover(t.Context(), before.ID, 2)
	require.ErrorIs(t, err, ErrDisabled)
	view, previous, err := Takeover(t.Context(), before.ID, 7)
	require.NoError(t, err)
	require.Equal(t, 2, previous)
	require.Equal(t, 7, view.CreatedBy)
	var after model.ManagedAccountAPI
	require.NoError(t, db.First(&after, before.ID).Error)
	require.Equal(t, before.Status, after.Status)
	require.Equal(t, before.Fields, after.Fields)
	require.Equal(t, before.Rules, after.Rules)
	require.Equal(t, before.IncludeTerms, after.IncludeTerms)
	require.Equal(t, before.PortalPasswordHash, after.PortalPasswordHash)
	var sessions int64
	require.NoError(t, db.Model(&model.ManagedAccountAPIPortalSession{}).Where("api_id = ?", before.ID).Count(&sessions).Error)
	require.Zero(t, sessions)
	_, err = Authenticate(created.Secret, "203.0.113.1")
	require.ErrorIs(t, err, ErrUnauthorized)
}
