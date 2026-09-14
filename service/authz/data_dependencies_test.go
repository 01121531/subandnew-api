package authz

import (
	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDerivedDataRequiresEveryInputGrant(t *testing.T) {
	for field, groups := range dataFieldDependencies {
		t.Run(field, func(t *testing.T) {
			a := &DataAccess{Role: common.RoleAdminUser, Policy: model.EmptyAdminDataPolicy()}
			for _, group := range groups {
				a.Policy.Fields[group] = true
			}
			require.NoError(t, a.CheckDataField(field))
			for _, group := range groups {
				a.Policy.Fields[group] = false
				require.ErrorIs(t, a.CheckDataField(field), ErrDataForbidden)
				projected, err := a.Project(map[string]any{field: 5})
				require.NoError(t, err)
				require.NotContains(t, projected, field)
				a.Policy.Fields[group] = true
			}
		})
	}
}
