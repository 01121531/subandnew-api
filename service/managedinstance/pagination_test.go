package managedinstance

import (
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
)

func TestInstanceListHasMoreSurvivesCountProjection(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	ids := seedLegacyOrder(t, db, 3)
	allowed := []int64{ids[0], ids[2]}
	access := &authz.DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
	for page := 1; page <= 3; page++ {
		result, err := List(ListFilter{AllowedIDs: &allowed, Page: page, PageSize: 1})
		require.NoError(t, err)
		require.Equal(t, int64(2), result.Total)
		require.Equal(t, page == 1, result.HasMore)
		if page <= 2 {
			require.Len(t, result.Items, 1)
			require.Equal(t, allowed[page-1], result.Items[0].Id)
		} else {
			require.Empty(t, result.Items)
		}
		projected, err := access.Project(result)
		require.NoError(t, err)
		data := projected.(map[string]any)
		require.NotContains(t, data, "total")
		require.Equal(t, page == 1, data["has_more"])
	}
	empty := []int64{}
	result, err := List(ListFilter{AllowedIDs: &empty, PageSize: 1})
	require.NoError(t, err)
	require.False(t, result.HasMore)
	require.Empty(t, result.Items)
}
