package access

import (
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestDefaultInstanceUpdatesValidateExistenceAndIdentityScope(t *testing.T) {
	db, execution := newAccessDB(t)
	allowed := model.ManagedInstance{Name: "allowed", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://allowed.example", Environment: "production"}
	denied := model.ManagedInstance{Name: "denied", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://denied.example", Environment: "production"}
	require.NoError(t, db.Create(&allowed).Error)
	require.NoError(t, db.Create(&denied).Error)
	require.NoError(t, db.Create(&model.AssistantIdentityInstanceScope{IdentityID: execution.IdentityID, InstanceID: allowed.Id}).Error)

	require.NoError(t, UpdateIdentityDefaultInstance(t.Context(), db, execution.IdentityID, &allowed.Id))
	var identity model.AssistantIdentity
	require.NoError(t, db.First(&identity, execution.IdentityID).Error)
	require.Equal(t, allowed.Id, *identity.DefaultInstanceID)
	require.ErrorIs(t, UpdateIdentityDefaultInstance(t.Context(), db, execution.IdentityID, &denied.Id), ErrInstanceDenied)
	require.NoError(t, UpdateIdentityDefaultInstance(t.Context(), db, execution.IdentityID, nil))
	require.NoError(t, db.First(&identity, execution.IdentityID).Error)
	require.Nil(t, identity.DefaultInstanceID)

	require.NoError(t, UpdateGlobalDefaultInstance(t.Context(), db, &denied.Id, execution.UserID))
	globalID, err := GetGlobalDefaultInstanceID(t.Context(), db)
	require.NoError(t, err)
	require.Equal(t, denied.Id, *globalID)
	missingID := denied.Id + 1000
	require.ErrorIs(t, UpdateGlobalDefaultInstance(t.Context(), db, &missingID, execution.UserID), ErrInstanceDenied)
}
