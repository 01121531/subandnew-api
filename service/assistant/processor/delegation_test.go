package processor

import (
	"bytes"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/secrets"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAuthorizationChangeExcludesHistoryAndQueuedReply(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AssistantIdentity{}, &model.AssistantIdentityInstanceScope{}, &model.AssistantMessage{}, &model.AssistantOutbox{}))
	root := model.User{Id: 1, Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&root).Error)
	identity := model.AssistantIdentity{ChannelID: 1, ExternalUserID: "peer", UserID: 1, BoundBy: 1, Status: model.AssistantIdentityStatusActive, AllowedInstanceScope: model.AssistantInstanceScopeAll}
	require.NoError(t, db.Create(&identity).Error)
	cipher, err := secrets.New(map[string][]byte{"v1": bytes.Repeat([]byte{7}, 32)}, "v1")
	require.NoError(t, err)
	p := &Processor{db: db, cipher: cipher, now: time.Now, ownerID: "test"}
	before, err := p.scopeFingerprint(t.Context(), &identity)
	require.NoError(t, err)
	require.NoError(t, p.storeMessage(t.Context(), 1, 1, 1, model.AssistantMessageRoleAssistant, before, "previously authorized private data"))
	event := model.AssistantInboundEvent{ID: 1, ChannelID: 1, PeerID: "peer"}
	require.NoError(t, p.enqueueReply(t.Context(), &event, 1, 1, "token", "previously authorized private data"))
	var beforeUser model.User
	require.NoError(t, db.First(&beforeUser, root.Id).Error)
	updated := db.Model(&model.User{}).Where("id = ?", root.Id).
		UpdateColumn("authorization_version", gorm.Expr("authorization_version + 1"))
	require.NoError(t, updated.Error)
	require.EqualValues(t, 1, updated.RowsAffected)
	var afterUser model.User
	require.NoError(t, db.First(&afterUser, root.Id).Error)
	require.Equal(t, beforeUser.AuthorizationVersion+1, afterUser.AuthorizationVersion)
	after, err := p.scopeFingerprint(t.Context(), &identity)
	require.NoError(t, err)
	require.NotEqual(t, before, after)
	history, err := p.loadConversationHistory(t.Context(), 1, after)
	require.NoError(t, err)
	require.Empty(t, history)
	var outbox model.AssistantOutbox
	require.NoError(t, db.First(&outbox).Error)
	// No channel client is configured: revoked replies must stop before delivery.
	require.Error(t, p.Deliver(t.Context(), outbox.ID))
	require.NoError(t, db.First(&outbox, outbox.ID).Error)
	require.Equal(t, "authorization_changed", outbox.ErrorCode)
}
