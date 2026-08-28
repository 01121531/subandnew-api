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
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AssistantIdentity{}, &model.AssistantIdentityInstanceScope{}, &model.ManagedInstance{}))
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
