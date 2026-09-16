package supplier

import (
	"net/url"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
)

func TestSupplierDelegatedOwnerScopeRevocationAndRootTakeover(t *testing.T) {
	f := newCoreFixture(t)
	require.NoError(t, f.db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	master := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = master })
	require.NoError(t, authz.Init(f.db))
	admin := model.User{Id: 2, Username: "responsible", Role: common.RoleAdminUser, Status: common.UserStatusEnabled}
	require.NoError(t, f.db.Create(&admin).Error)
	require.NoError(t, authz.SetUserPermissions(2, authz.PermissionsMap{authz.ResourceSupplierManagement: {"manage": true}, authz.ResourceManagedInstance: {"view": true}}))
	first, second := f.instance(t, 1), f.instance(t, 2)
	policy := model.EmptyAdminDataPolicy()
	policy.UserID, policy.InstanceScope, policy.InstanceIDs = 2, "selected", []int64{first.Id}
	require.NoError(t, f.db.Create(&policy).Error)
	item := f.supplier(t, "delegated", false)
	allowed := f.binding(t, item.ID, first.Id)
	denied := f.binding(t, item.ID, second.Id)
	require.NoError(t, f.db.Model(item).UpdateColumn("responsible_admin_id", 2).Error)
	p, token := f.login(t, item.Username)
	bindings, err := f.s.PortalBindings(item.ID)
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.False(t, bindings[0].EffectivePolicy.Values["account.email"])
	_, err = f.s.Read(t.Context(), p, denied.ID, "accounts", url.Values{}, false)
	require.Error(t, err)
	_, err = f.s.Read(t.Context(), p, allowed.ID, "accounts", url.Values{"search": {"private"}}, false)
	require.Error(t, err)
	_, err = f.s.Read(t.Context(), p, allowed.ID, "accounts", url.Values{}, false)
	require.NoError(t, err)
	f.r.onRead = func() {
		require.NoError(t, authz.SetUserPermissions(2, authz.PermissionsMap{authz.ResourceSupplierManagement: {"manage": false}}))
	}
	_, err = f.s.Read(t.Context(), p, allowed.ID, "accounts", url.Values{}, true)
	require.Error(t, err)
	_, err = f.s.Authenticate(token)
	require.Error(t, err)
	require.Error(t, f.s.Takeover(t.Context(), item.ID, 2, "127.0.0.1"))
	require.NoError(t, f.s.Takeover(t.Context(), item.ID, 1, "127.0.0.1"))
	_, err = f.s.Authenticate(token)
	require.Error(t, err)
	var audits []model.SupplierAudit
	require.NoError(t, f.db.Where("action = ?", "supplier.takeover").Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, 1, audits[0].AdminID)
}

func TestSupplierOwnerBackfillFreshSetupAndMissingRoot(t *testing.T) {
	f := newCoreFixture(t)
	require.NoError(t, f.db.Delete(&model.User{}, 1).Error)
	require.NoError(t, BackfillLegacyOwners(f.db))
	require.NoError(t, f.db.Create(&model.Supplier{Name: "legacy", Username: "legacy", PasswordHash: "hash", Enabled: true}).Error)
	require.Error(t, BackfillLegacyOwners(f.db))
}

func TestSupplierRuntimeMetricsRespectResponsibleAdmin(t *testing.T) {
	for _, field := range []string{"rpm", "tokens", "concurrency"} {
		a := &authz.DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
		a.Policy.Fields[field] = true
		policy := &model.SupplierEffectivePolicy{Values: map[string]bool{"account.rpm": true, "account.tpm": true, "account.concurrent": true, "account.active_sessions": true}, Sources: map[string]string{}}
		intersectPolicy(policy, a)
		require.Equal(t, field == "rpm", policy.Values["account.rpm"])
		require.Equal(t, field == "tokens", policy.Values["account.tpm"])
		require.Equal(t, field == "concurrency", policy.Values["account.concurrent"])
		require.Equal(t, field == "concurrency", policy.Values["account.active_sessions"])
	}
}
