package model

import (
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func adminPolicyContractDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&User{}, &AdminDataPolicy{}, &ManagedInstance{}))
	return db
}

func TestRBACContractPolicyMigrationLegacyFullAndNewDeny(t *testing.T) {
	db := adminPolicyContractDB(t)
	for _, user := range []User{
		{Id: 1, Username: "legacy-admin", Role: common.RoleAdminUser},
		{Id: 2, Username: "restricted-admin", Role: common.RoleAdminUser},
		{Id: 3, Username: "root", Role: common.RoleRootUser},
		{Id: 4, Username: "ordinary", Role: common.RoleCommonUser},
		{Id: 5, Username: "disabled-admin", Role: common.RoleAdminUser, Status: common.UserStatusDisabled},
		{Id: 6, Username: "deleted-admin", Role: common.RoleAdminUser},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	require.NoError(t, db.Delete(&User{}, 6).Error)
	existing := EmptyAdminDataPolicy()
	existing.UserID, existing.Revision = 2, 7
	existing.Fields["email"] = false
	require.NoError(t, db.Create(&existing).Error)
	require.NoError(t, MigrateAdminDataPolicies(db))
	for _, id := range []int{1, 5} {
		var got AdminDataPolicy
		require.NoError(t, db.First(&got, "user_id = ?", id).Error)
		want := FullAdminDataPolicy()
		want.UserID = id
		assert.Equal(t, want, got)
	}
	newUser := User{Id: 7, Username: "new-admin", Role: common.RoleAdminUser}
	require.NoError(t, db.Create(&newUser).Error)
	newPolicy := EmptyAdminDataPolicy()
	newPolicy.UserID, newPolicy.Revision = newUser.Id, 1
	require.NoError(t, db.Create(&newPolicy).Error)
	require.NoError(t, MigrateAdminDataPolicies(db))
	for _, want := range []AdminDataPolicy{existing, newPolicy} {
		var got AdminDataPolicy
		require.NoError(t, db.First(&got, "user_id = ?", want.UserID).Error)
		assert.Equal(t, want, got, "restarts must not widen existing or new deny policies")
	}
	var ids []int
	require.NoError(t, db.Model(&AdminDataPolicy{}).Order("user_id").Pluck("user_id", &ids).Error)
	assert.Equal(t, []int{1, 2, 5, 7}, ids)
}

func TestRBACContractPolicyValidation(t *testing.T) {
	db := adminPolicyContractDB(t)
	require.NoError(t, db.Create(&ManagedInstance{Id: 1, Name: "one", Kind: ManagedInstanceKindGeneric, BaseURL: "https://example.test"}).Error)
	for _, tc := range []struct {
		name   string
		policy AdminDataPolicy
		valid  bool
	}{
		{"empty-selected", EmptyAdminDataPolicy(), true},
		{"full", FullAdminDataPolicy(), true},
		{"selected-existing", AdminDataPolicy{InstanceScope: "selected", InstanceIDs: []int64{1}, Fields: map[string]bool{"email": false}}, true},
		{"invalid-scope", AdminDataPolicy{InstanceScope: "any"}, false},
		{"negative-revision", AdminDataPolicy{InstanceScope: "selected", Revision: -1}, false},
		{"all-with-ids", AdminDataPolicy{InstanceScope: "all", InstanceIDs: []int64{1}}, false},
		{"unknown-denied-field", AdminDataPolicy{InstanceScope: "selected", Fields: map[string]bool{"typo": false}}, false},
		{"zero-id", AdminDataPolicy{InstanceScope: "selected", InstanceIDs: []int64{0}}, false},
		{"negative-id", AdminDataPolicy{InstanceScope: "selected", InstanceIDs: []int64{-1}}, false},
		{"duplicate-id", AdminDataPolicy{InstanceScope: "selected", InstanceIDs: []int64{1, 1}}, false},
		{"missing-id", AdminDataPolicy{InstanceScope: "selected", InstanceIDs: []int64{1, 99}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate(db)
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestRBACContractPolicyConstructorsDoNotShareGrants(t *testing.T) {
	full := FullAdminDataPolicy()
	full.Fields["email"] = false
	assert.True(t, FullAdminDataPolicy().Fields["email"])
	empty := EmptyAdminDataPolicy()
	empty.Fields["email"] = true
	assert.Empty(t, EmptyAdminDataPolicy().Fields)
	assert.NotNil(t, EmptyAdminDataPolicy().InstanceIDs)
}
