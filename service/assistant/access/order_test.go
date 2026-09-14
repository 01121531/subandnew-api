package access

import (
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/stretchr/testify/require"
)

func TestInstanceOptionsFollowGlobalOrderWithinScope(t *testing.T) {
	db, execution := newAccessDB(t)
	actor, err := authz.LoadDataAccess(db, execution.UserID)
	require.NoError(t, err)
	ctx := authz.WithDataAccess(t.Context(), actor)
	instances := []model.ManagedInstance{
		{Name: "A", BaseURL: "https://a.example", Kind: model.ManagedInstanceKindGeneric},
		{Name: "Z", BaseURL: "https://z.example", Kind: model.ManagedInstanceKindGeneric},
		{Name: "B", BaseURL: "https://b.example", Kind: model.ManagedInstanceKindGeneric, SortOrder: 1},
	}
	for i := range instances {
		require.NoError(t, db.Create(&instances[i]).Error)
	}
	options, err := ListAllInstanceOptions(ctx, db)
	require.NoError(t, err)
	require.Equal(t, []int64{instances[1].Id, instances[0].Id, instances[2].Id}, optionIDs(options))
	require.NoError(t, db.Model(&instances[0]).UpdateColumn("sort_order", 2).Error)
	options, err = ListAllInstanceOptions(ctx, db)
	require.NoError(t, err)
	require.Equal(t, []int64{instances[1].Id, instances[2].Id, instances[0].Id}, optionIDs(options))
	var identity model.AssistantIdentity
	require.NoError(t, db.First(&identity, execution.IdentityID).Error)
	for _, i := range []int{0, 2} {
		require.NoError(t, db.Create(&model.AssistantIdentityInstanceScope{IdentityID: identity.ID, InstanceID: instances[i].Id}).Error)
	}
	options, err = ListIdentityInstanceOptions(ctx, db, &identity)
	require.NoError(t, err)
	require.Equal(t, []int64{instances[2].Id, instances[0].Id}, optionIDs(options))
}

func optionIDs(options []InstanceOption) []int64 {
	ids := make([]int64, 0, len(options))
	for _, option := range options {
		ids = append(ids, option.ID)
	}
	return ids
}
