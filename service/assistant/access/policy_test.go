package access

import (
	"context"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newAccessDB(t *testing.T) (*gorm.DB, tool.ExecutionContext) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AssistantIdentity{}, &model.AssistantIdentityInstanceScope{}, &model.AssistantSetting{}, &model.ManagedInstance{}))
	user := model.User{Username: "assistant-root", Password: "hash", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	identity := model.AssistantIdentity{ChannelID: 1, ExternalUserID: "wx-user", UserID: user.Id, Status: model.AssistantIdentityStatusActive, AllowedInstanceScope: model.AssistantInstanceScopeSelected}
	require.NoError(t, db.Create(&identity).Error)
	return db, tool.ExecutionContext{RunID: "run", ConversationID: "conversation", Channel: "wechat", IdentityID: identity.ID, UserID: user.Id, UserRole: user.Role}
}

func TestAuthorizeReloadsCurrentUserAndIdentity(t *testing.T) {
	db, execution := newAccessDB(t)
	authorize := Authorize(db)
	request := tool.AuthorizationRequest{Execution: execution, Tool: tool.ToolSpec{Permission: tool.Permission{Resource: authz.ResourceManagedInstance, Action: authz.ManagedInstanceActionView}}}
	require.NoError(t, authorize(context.Background(), request))

	require.NoError(t, db.Model(&model.User{}).Where("id = ?", execution.UserID).Update("status", common.UserStatusDisabled).Error)
	require.ErrorIs(t, authorize(context.Background(), request), ErrUserDisabled)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", execution.UserID).Updates(map[string]any{"status": common.UserStatusEnabled, "role": common.RoleCommonUser}).Error)
	require.Error(t, authorize(context.Background(), request))
}

func TestResolveInstanceIDsFailsClosedForSelectedScope(t *testing.T) {
	db, execution := newAccessDB(t)
	first := model.ManagedInstance{Name: "first", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://first.example", Environment: "production"}
	second := model.ManagedInstance{Name: "second", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://second.example", Environment: "production"}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)
	require.NoError(t, db.Create(&model.AssistantIdentityInstanceScope{IdentityID: execution.IdentityID, InstanceID: first.Id}).Error)

	ids, err := ResolveInstanceIDs(t.Context(), db, execution, nil)
	require.NoError(t, err)
	require.Equal(t, []int64{first.Id}, ids)
	_, err = ResolveInstanceIDs(t.Context(), db, execution, []int64{second.Id})
	require.ErrorIs(t, err, ErrInstanceDenied)
}

func TestResolveInstanceIDsAllScopeExpandsVisibleInstances(t *testing.T) {
	db, execution := newAccessDB(t)
	require.NoError(t, db.Model(&model.AssistantIdentity{}).Where("id = ?", execution.IdentityID).Update("allowed_instance_scope", model.AssistantInstanceScopeAll).Error)
	instance := model.ManagedInstance{Name: "one", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://one.example", Environment: "production"}
	require.NoError(t, db.Create(&instance).Error)

	ids, err := ResolveInstanceIDs(t.Context(), db, execution, nil)
	require.NoError(t, err)
	require.Equal(t, []int64{instance.Id}, ids)
}

func TestResolveInstanceSelectionPrefersPersonalThenGlobalDefault(t *testing.T) {
	db, execution := newAccessDB(t)
	personal := model.ManagedInstance{Name: "personal", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://personal.example", Environment: "production"}
	global := model.ManagedInstance{Name: "global", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://global.example", Environment: "production"}
	require.NoError(t, db.Create(&personal).Error)
	require.NoError(t, db.Create(&global).Error)
	require.NoError(t, db.Create(&model.AssistantIdentityInstanceScope{IdentityID: execution.IdentityID, InstanceID: personal.Id}).Error)
	require.NoError(t, db.Create(&model.AssistantIdentityInstanceScope{IdentityID: execution.IdentityID, InstanceID: global.Id}).Error)
	require.NoError(t, db.Model(&model.AssistantIdentity{}).Where("id = ?", execution.IdentityID).Update("default_instance_id", personal.Id).Error)
	require.NoError(t, db.Create(&model.AssistantSetting{ID: 1, GlobalDefaultInstanceID: &global.Id}).Error)

	resolution, err := ResolveInstanceSelection(t.Context(), db, execution, nil, InstanceSelectionDefault)
	require.NoError(t, err)
	require.Equal(t, []int64{personal.Id}, resolution.IDs)
	require.Equal(t, DefaultSourcePersonal, resolution.Source)
	require.False(t, resolution.Fallback)

	require.NoError(t, db.Where("identity_id = ? AND instance_id = ?", execution.IdentityID, personal.Id).Delete(&model.AssistantIdentityInstanceScope{}).Error)
	resolution, err = ResolveInstanceSelection(t.Context(), db, execution, nil, InstanceSelectionDefault)
	require.NoError(t, err)
	require.Equal(t, []int64{global.Id}, resolution.IDs)
	require.Equal(t, DefaultSourceGlobal, resolution.Source)
	require.True(t, resolution.Fallback)
}

func TestResolveInstanceSelectionFallsBackToAllAllowed(t *testing.T) {
	db, execution := newAccessDB(t)
	allowed := model.ManagedInstance{Name: "allowed", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://allowed.example", Environment: "production"}
	unavailable := model.ManagedInstance{Name: "unavailable", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://unavailable.example", Environment: "production"}
	require.NoError(t, db.Create(&allowed).Error)
	require.NoError(t, db.Create(&unavailable).Error)
	require.NoError(t, db.Create(&model.AssistantIdentityInstanceScope{IdentityID: execution.IdentityID, InstanceID: allowed.Id}).Error)
	require.NoError(t, db.Model(&model.AssistantIdentity{}).Where("id = ?", execution.IdentityID).Update("default_instance_id", unavailable.Id).Error)
	require.NoError(t, db.Create(&model.AssistantSetting{ID: 1, GlobalDefaultInstanceID: &unavailable.Id}).Error)

	resolution, err := ResolveInstanceSelection(t.Context(), db, execution, nil, InstanceSelectionDefault)
	require.NoError(t, err)
	require.Equal(t, []int64{allowed.Id}, resolution.IDs)
	require.Equal(t, DefaultSourceAll, resolution.Source)
	require.True(t, resolution.Fallback)
}

func TestResolveInstanceSelectionExplicitRequestOverridesDefault(t *testing.T) {
	db, execution := newAccessDB(t)
	first := model.ManagedInstance{Name: "first", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://first.example", Environment: "production"}
	second := model.ManagedInstance{Name: "second", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://second.example", Environment: "production"}
	hidden := model.ManagedInstance{Name: "hidden", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://hidden.example", Environment: "production"}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)
	require.NoError(t, db.Create(&hidden).Error)
	require.NoError(t, db.Create(&model.AssistantIdentityInstanceScope{IdentityID: execution.IdentityID, InstanceID: first.Id}).Error)
	require.NoError(t, db.Create(&model.AssistantIdentityInstanceScope{IdentityID: execution.IdentityID, InstanceID: second.Id}).Error)
	require.NoError(t, db.Model(&model.AssistantIdentity{}).Where("id = ?", execution.IdentityID).Update("default_instance_id", first.Id).Error)

	resolution, err := ResolveInstanceSelection(t.Context(), db, execution, []int64{second.Id}, InstanceSelectionDefault)
	require.NoError(t, err)
	require.Equal(t, []int64{second.Id}, resolution.IDs)
	require.Equal(t, "explicit", resolution.Source)

	resolution, err = ResolveInstanceSelection(t.Context(), db, execution, nil, InstanceSelectionAll)
	require.NoError(t, err)
	require.Equal(t, []int64{first.Id, second.Id}, resolution.IDs)

	_, err = ResolveInstanceSelection(t.Context(), db, execution, []int64{hidden.Id}, InstanceSelectionDefault)
	require.ErrorIs(t, err, ErrInstanceDenied)
	_, err = ResolveInstanceSelection(t.Context(), db, execution, []int64{first.Id}, InstanceSelectionAll)
	require.ErrorIs(t, err, ErrInstanceDenied)
}
