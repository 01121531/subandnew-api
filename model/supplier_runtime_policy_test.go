package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSupplierRuntimeMigrationPreservesMetricRestrictions(t *testing.T) {
	db := supplierModelDB(t)
	require.NoError(t, db.AutoMigrate(&SupplierPolicyDefault{}))
	on, off := true, false
	old := InitialSupplierPolicy()
	for key := range SupplierRuntimePolicyParents {
		delete(old, key)
	}
	old["summary.rpm"] = &off
	defaults := SupplierPolicyDefault{ID: 1, Revision: 7, Policy: old}
	require.NoError(t, db.Create(&defaults).Error)
	owner := Supplier{Username: "runtime", PolicyOverrides: SupplierPolicy{"account.total_tokens": &off, "summary.pool_concurrent": &off}, PolicyVersion: 3}
	require.NoError(t, db.Create(&owner).Error)
	binding := SupplierBinding{SupplierID: owner.ID, InstanceID: 1, RemoteUserID: "vendor", PolicyOverrides: SupplierPolicy{"summary.rpm": &on}, PolicyVersion: 4}
	require.NoError(t, db.Create(&binding).Error)
	for i := 0; i < 2; i++ {
		require.NoError(t, MigrateSupplierPolicies(db))
		require.NoError(t, db.First(&defaults, 1).Error)
		require.NoError(t, db.First(&owner, owner.ID).Error)
		require.NoError(t, db.First(&binding, binding.ID).Error)
		require.EqualValues(t, 8, defaults.Revision)
		require.EqualValues(t, 4, owner.PolicyVersion)
		require.EqualValues(t, 5, binding.PolicyVersion)
		policy := ResolveSupplierPolicy(defaults, owner, &binding)
		require.True(t, policy.Values["account.rpm"])
		require.Equal(t, "binding", policy.Sources["account.rpm"])
		for _, key := range []string{"account.tpm", "account.concurrent", "account.active_sessions"} {
			require.False(t, policy.Values[key], key)
		}
		require.False(t, ResolveSupplierPolicy(defaults, Supplier{PolicyOverrides: SupplierPolicy{}}, nil).Values["account.rpm"])
	}
}
