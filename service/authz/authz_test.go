package authz

import (
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAuthzTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = wasMaster })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	return db
}

func TestInitSeedsControlPlanePoliciesOnce(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))
	require.NoError(t, Init(db))

	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Count(&count).Error)
	assert.Equal(t, int64(len(PermissionsForRole(BuiltInRoleAdmin))), count)
	assert.True(t, Can(1, common.RoleRootUser, ManagedInstanceSecretRotate))
	assert.True(t, Can(2, common.RoleAdminUser, ManagedInstanceView))
	assert.True(t, Can(2, common.RoleAdminUser, ManagedInstanceUsageView))
	assert.False(t, Can(2, common.RoleAdminUser, ManagedInstanceOperate))
	assert.False(t, Can(3, common.RoleCommonUser, ManagedInstanceView))
}

func TestSetUserPermissionsStoresOnlyControlPlaneOverrides(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	require.NoError(t, SetUserPermissions(42, PermissionsMap{
		ResourceManagedInstance: {
			ManagedInstanceActionView:         true,
			ManagedInstanceActionUpdate:       true,
			ManagedInstanceActionSecretRotate: true,
			"unknown":                         true,
		},
		"unknown": {ManagedInstanceActionView: true},
	}))

	assert.True(t, Can(42, common.RoleAdminUser, ManagedInstanceSecretRotate))
	assert.True(t, Can(42, common.RoleAdminUser, ManagedInstanceUpdate))
	assert.Equal(t, PermissionsMap{
		ResourceManagedInstance: {
			ManagedInstanceActionUpdate:       true,
			ManagedInstanceActionSecretRotate: true,
		},
	}, ExplicitUserOverrides(42))
}

func TestClearUserAuthorizationRemovesOverrides(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))
	require.NoError(t, SetUserPermissions(90, PermissionsMap{
		ResourceManagedInstance: {ManagedInstanceActionSecretRotate: true},
	}))

	assert.True(t, Can(90, common.RoleAdminUser, ManagedInstanceSecretRotate))
	require.NoError(t, ClearUserAuthorization(90))
	assert.Empty(t, ExplicitUserOverrides(90))
	assert.False(t, Can(90, common.RoleAdminUser, ManagedInstanceSecretRotate))
}

func TestAdapterAddPolicyIsIdempotent(t *testing.T) {
	db := newAuthzTestDB(t)
	adapter := newGormAdapter(db)
	rule := []string{UserSubject(55), ResourceManagedInstance, ManagedInstanceActionSecretRotate, EffectAllow}
	require.NoError(t, adapter.AddPolicy("p", "p", rule))
	require.NoError(t, adapter.AddPolicy("p", "p", rule))

	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("ptype = ? AND v0 = ?", "p", UserSubject(55)).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
