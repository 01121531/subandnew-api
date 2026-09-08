package authz

import (
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/stretchr/testify/require"
)

func TestSupplierPermissionsRequireExplicitAdministratorGrants(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))
	for _, permission := range []Permission{SupplierView, SupplierManage, SupplierAudit} {
		require.True(t, Can(1, common.RoleRootUser, permission))
		require.False(t, Can(42, common.RoleAdminUser, permission))
		require.False(t, Can(43, common.RoleCommonUser, permission))
	}
	require.NoError(t, SetUserPermissions(42, PermissionsMap{ResourceSupplierManagement: {"view": true}}))
	require.True(t, Can(42, common.RoleAdminUser, SupplierView))
	require.False(t, Can(42, common.RoleAdminUser, SupplierManage))
	require.False(t, Can(42, common.RoleAdminUser, SupplierAudit))
	require.NoError(t, SetUserPermissions(42, PermissionsMap{ResourceSupplierManagement: {"view": true, "manage": true, "audit": true}}))
	require.True(t, Can(42, common.RoleAdminUser, SupplierManage))
	require.True(t, Can(42, common.RoleAdminUser, SupplierAudit))
}
