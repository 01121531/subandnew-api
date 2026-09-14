package access

import (
	"encoding/json"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
)

func TestDelegatedAssistantScopeDefaultsFieldsAndRevocation(t *testing.T) {
	db, execution := newAccessDB(t)
	require.NoError(t, db.AutoMigrate(&model.AdminDataPolicy{}, &model.CasbinRule{}, &model.AuthzRole{}))
	master := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = master })
	require.NoError(t, authz.Init(db))
	rootID := execution.UserID
	user := model.User{Username: "restricted", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, authz.SetUserPermissions(user.Id, authz.PermissionsMap{authz.ResourceAssistant: {"access": true}, authz.ResourceManagedInstance: {"view": true}}))
	first := model.ManagedInstance{Name: "allowed", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://allowed.invalid"}
	second := model.ManagedInstance{Name: "hidden-default", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://hidden.invalid"}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)
	policy := model.EmptyAdminDataPolicy()
	policy.UserID, policy.InstanceScope, policy.InstanceIDs = user.Id, "selected", []int64{first.Id}
	policy.Fields["requests"] = true
	require.NoError(t, db.Create(&policy).Error)
	require.NoError(t, db.Model(&model.AssistantIdentity{}).Where("id = ?", execution.IdentityID).Updates(map[string]any{"user_id": user.Id, "bound_by": rootID, "allowed_instance_scope": model.AssistantInstanceScopeAll, "default_instance_id": second.Id}).Error)
	execution.UserID, execution.UserRole = user.Id, user.Role
	resolution, err := ResolveInstanceSelection(t.Context(), db, execution, nil, "")
	require.NoError(t, err)
	require.Equal(t, []int64{first.Id}, resolution.IDs)
	require.Empty(t, resolution.DefaultName)
	_, err = ResolveInstanceIDs(t.Context(), db, execution, []int64{second.Id})
	require.ErrorIs(t, err, ErrInstanceDenied)
	bounded, finish, err := Boundary(db)(t.Context(), execution)
	require.NoError(t, err)
	a := authz.DataAccessFrom(bounded)
	require.NotNil(t, a)
	require.Error(t, checkArguments(a, json.RawMessage(`{"metrics":["cost"]}`)))
	require.Error(t, checkArguments(a, json.RawMessage(`{"rules":[{"field":"email","values":["private"]}]}`)))
	result, err := finish(tool.Result{Data: json.RawMessage(`{"items":[{"name":"allowed","email":"secret","requests":12,"amount":99}]}`)})
	require.NoError(t, err)
	require.Contains(t, string(result.Data), `"requests":12`)
	require.NotContains(t, string(result.Data), `"email"`)
	require.NotContains(t, string(result.Data), `"amount"`)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).UpdateColumn("authorization_version", a.Version+1).Error)
	_, err = finish(tool.Result{Data: json.RawMessage(`{}`)})
	require.ErrorIs(t, err, authz.ErrAuthorizationChanged)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", rootID).Update("status", common.UserStatusDisabled).Error)
	_, err = ResolveInstanceIDs(t.Context(), db, execution, nil)
	require.ErrorIs(t, err, ErrIdentityDenied)
}

func TestAssistantConfigOptionsRequireAuthorizedContext(t *testing.T) {
	db, execution := newAccessDB(t)
	_, err := ListAllInstanceOptions(t.Context(), db)
	require.ErrorIs(t, err, ErrInstanceDenied)
	a, err := authz.LoadDataAccess(db, execution.UserID)
	require.NoError(t, err)
	options, err := ListAllInstanceOptions(authz.WithDataAccess(t.Context(), a), db)
	require.NoError(t, err)
	require.Empty(t, options)
}
