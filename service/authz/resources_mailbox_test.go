package authz

import (
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/stretchr/testify/require"
)

func TestMailboxPermissionsAreExplicitAndIndependent(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))
	permissions := []Permission{MailboxView, MailboxManage, MailboxAssign, MailboxReview, MailboxCredentials, MailboxOperators, MailboxAudit}
	for _, permission := range permissions {
		require.True(t, Can(1, common.RoleRootUser, permission))
		require.False(t, Can(42, common.RoleAdminUser, permission))
		require.False(t, Can(43, common.RoleCommonUser, permission))
	}
	require.NoError(t, SetUserPermissions(42, PermissionsMap{ResourceMailboxManagement: {"view": true, "review": true}}))
	require.True(t, Can(42, common.RoleAdminUser, MailboxView))
	require.True(t, Can(42, common.RoleAdminUser, MailboxReview))
	for _, permission := range []Permission{MailboxManage, MailboxAssign, MailboxCredentials, MailboxOperators, MailboxAudit} {
		require.False(t, Can(42, common.RoleAdminUser, permission))
	}
}
