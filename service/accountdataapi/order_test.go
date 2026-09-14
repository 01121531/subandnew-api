package accountdataapi

import (
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestInstanceOptionsFollowGlobalOrder(t *testing.T) {
	db, first := setupAPIServiceTest(t)
	second := model.ManagedInstance{Name: "Z-last-by-name", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://second.example"}
	appended := model.ManagedInstance{Name: "A-first-by-name", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://appended.example", SortOrder: 1}
	require.NoError(t, db.Create(&second).Error)
	require.NoError(t, db.Create(&appended).Error)
	options, err := ListInstanceOptions()
	require.NoError(t, err)
	require.Len(t, options, 3)
	require.Equal(t, second.Id, options[0].ID)
	require.Equal(t, first.Id, options[1].ID)
	require.Equal(t, appended.Id, options[2].ID)
	require.NoError(t, db.Model(&first).UpdateColumn("sort_order", 2).Error)
	options, err = ListInstanceOptions()
	require.NoError(t, err)
	require.Equal(t, second.Id, options[0].ID)
	require.Equal(t, appended.Id, options[1].ID)
	require.Equal(t, first.Id, options[2].ID)
}
