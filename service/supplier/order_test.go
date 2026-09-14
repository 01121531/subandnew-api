package supplier

import (
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestBindingsFollowGlobalOrderWithoutChangingAliases(t *testing.T) {
	f := newCoreFixture(t)
	owner := f.supplier(t, "order-owner", true)
	first, second, appended := f.instance(t, 1), f.instance(t, 2), f.instance(t, 3)
	bindings := []*model.SupplierBinding{
		f.binding(t, owner.ID, first.Id), f.binding(t, owner.ID, second.Id), f.binding(t, owner.ID, appended.Id),
	}
	alias := "Supplier private alias"
	require.NoError(t, f.db.Model(bindings[0]).UpdateColumn("display_name", alias).Error)
	require.NoError(t, f.db.Model(appended).UpdateColumn("sort_order", 1).Error)
	for _, active := range []bool{false, true} {
		items, err := f.s.Bindings(owner.ID, active)
		require.NoError(t, err)
		require.Equal(t, []int64{second.Id, first.Id, appended.Id}, bindingInstanceIDs(items))
		require.Equal(t, &alias, items[1].DisplayName)
		require.Equal(t, first.Name, items[1].InstanceName)
	}
	require.NoError(t, f.db.Model(first).UpdateColumn("sort_order", 3).Error)
	require.NoError(t, f.db.Model(second).UpdateColumn("sort_order", 2).Error)
	items, err := f.s.Bindings(owner.ID, true)
	require.NoError(t, err)
	require.Equal(t, []int64{appended.Id, second.Id, first.Id}, bindingInstanceIDs(items))
	require.Equal(t, &alias, items[2].DisplayName)
	var stored model.SupplierBinding
	require.NoError(t, f.db.First(&stored, bindings[0].ID).Error)
	require.Equal(t, &alias, stored.DisplayName)
	require.Equal(t, bindings[0].Revision, stored.Revision)
	require.Equal(t, bindings[0].NamingVersion, stored.NamingVersion)
}

func bindingInstanceIDs(items []model.SupplierBinding) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.InstanceID)
	}
	return ids
}
