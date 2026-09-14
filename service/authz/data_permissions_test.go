package authz

import (
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestDataPermissionRevisionRefreshesAnotherNodesPolicyChange(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))
	a := &DataAccess{UserID: 72, Role: common.RoleAdminUser, Version: 1}
	require.True(t, a.Can(ManagedInstanceView))
	// Simulate another node committing an override without reloading this enforcer.
	require.NoError(t, db.Create(&model.CasbinRule{Ptype: "p", V0: UserSubject(72), V1: ResourceManagedInstance, V2: ManagedInstanceActionView, V3: EffectDeny}).Error)
	require.True(t, Can(72, common.RoleAdminUser, ManagedInstanceView))
	a.Version++
	require.False(t, a.Can(ManagedInstanceView))
	require.NoError(t, db.Where("v0 = ?", UserSubject(72)).Delete(&model.CasbinRule{}).Error)
	a.Version++
	require.True(t, a.Can(ManagedInstanceView))
}
