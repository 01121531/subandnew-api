package binding

import (
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newBindingService(t *testing.T) (*Service, *gorm.DB, model.User, model.ManagedInstance) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.ManagedInstance{}, &model.AssistantBindingCode{},
		&model.AssistantIdentity{}, &model.AssistantIdentityInstanceScope{},
	))
	user := model.User{Username: "root", Password: "hash", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	instance := model.ManagedInstance{Name: "prod", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://prod.example", Environment: "production"}
	require.NoError(t, db.Create(&instance).Error)
	service, err := NewService(db)
	require.NoError(t, err)
	return service, db, user, instance
}

func TestBindingCodeIsSingleUseAndCreatesSelectedScope(t *testing.T) {
	service, db, user, instance := newBindingService(t)
	generated, err := service.CreateCode(t.Context(), CreateInput{
		UserID: user.Id, CreatedBy: user.Id, Scope: model.AssistantInstanceScopeSelected, InstanceIDs: []int64{instance.Id},
	})
	require.NoError(t, err)
	require.Len(t, generated.Code, 9)
	var row model.AssistantBindingCode
	require.NoError(t, db.First(&row).Error)
	require.NotContains(t, row.CodeHash, generated.Code)

	identity, err := service.Consume(t.Context(), 10, "wx-user", generated.Code)
	require.NoError(t, err)
	require.Equal(t, model.AssistantIdentityStatusActive, identity.Status)
	var scopes []model.AssistantIdentityInstanceScope
	require.NoError(t, db.Where("identity_id = ?", identity.ID).Find(&scopes).Error)
	require.Equal(t, instance.Id, scopes[0].InstanceID)
	_, err = service.Consume(t.Context(), 10, "wx-user", generated.Code)
	require.ErrorIs(t, err, ErrCodeInvalid)
}

func TestBindingCodeExpiresAndCannotSwitchIdentity(t *testing.T) {
	service, db, user, _ := newBindingService(t)
	service.now = func() time.Time { return time.Unix(100, 0) }
	generated, err := service.CreateCode(t.Context(), CreateInput{UserID: user.Id, CreatedBy: user.Id, Scope: model.AssistantInstanceScopeAll})
	require.NoError(t, err)
	service.now = func() time.Time { return time.Unix(401, 0) }
	_, err = service.Consume(t.Context(), 10, "wx-user", generated.Code)
	require.ErrorIs(t, err, ErrCodeInvalid)

	other := model.User{Username: "other", Password: "hash", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&other).Error)
	service.now = func() time.Time { return time.Unix(500, 0) }
	generated, err = service.CreateCode(t.Context(), CreateInput{UserID: other.Id, CreatedBy: other.Id, Scope: model.AssistantInstanceScopeAll})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.AssistantIdentity{ChannelID: 10, ExternalUserID: "wx-user", UserID: user.Id, Status: model.AssistantIdentityStatusActive}).Error)
	_, err = service.Consume(t.Context(), 10, "wx-user", generated.Code)
	require.ErrorIs(t, err, ErrIdentityBound)
}
