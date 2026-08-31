package worker

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/channelservice"
	"github.com/01121531/subandnew-api/service/assistant/processor"
	"github.com/01121531/subandnew-api/service/assistant/provider"
	"github.com/01121531/subandnew-api/service/assistant/secrets"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelLeaseAllowsOnlyOneOwnerUntilExpiry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.AssistantChannelLease{}))
	cipher, err := secrets.New(map[string][]byte{"v1": bytes.Repeat([]byte{1}, 32)}, "v1")
	require.NoError(t, err)
	channels, err := channelservice.NewService(db, cipher, channelservice.Config{})
	require.NoError(t, err)
	dummyModel := func(context.Context) (provider.Client, *model.AssistantModelProfile, error) { return nil, nil, nil }
	dummyRegistry := func() (*tool.Registry, error) { return nil, nil }
	messageProcessor, err := processor.New(db, cipher, channels, dummyModel, dummyRegistry)
	require.NoError(t, err)
	first, err := New(db, channels, messageProcessor, "owner-a")
	require.NoError(t, err)
	second, err := New(db, channels, messageProcessor, "owner-b")
	require.NoError(t, err)
	now := time.Unix(100, 0)
	first.now = func() time.Time { return now }
	second.now = func() time.Time { return now }

	acquired, firstToken, err := first.acquireLease(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, _, err = second.acquireLease(t.Context(), 1)
	require.NoError(t, err)
	require.False(t, acquired)

	second.now = func() time.Time { return now.Add(leaseDuration + time.Second) }
	acquired, secondToken, err := second.acquireLease(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, acquired)
	require.Greater(t, secondToken, firstToken)
}

func TestExpiredOutboxSendBecomesUnknownInsteadOfBlindRetry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.AssistantOutbox{}))
	outbox := model.AssistantOutbox{ChannelID: 1, ReplyKey: "reply", Payload: "encrypted", Status: model.AssistantOutboxStatusSending, ClaimOwner: "dead-node", LeaseUntil: 99, DeliveryStartedAt: 90}
	require.NoError(t, db.Create(&outbox).Error)
	legacy := model.AssistantOutbox{ChannelID: 1, ReplyKey: "legacy-reply", Payload: "encrypted", Status: model.AssistantOutboxStatusSending}
	require.NoError(t, db.Create(&legacy).Error)
	worker := &Worker{db: db}
	worker.reconcileExpiredOutboxClaims(t.Context(), 100)
	require.NoError(t, db.First(&outbox, outbox.ID).Error)
	require.Equal(t, model.AssistantOutboxStatusUnknown, outbox.Status)
	require.Equal(t, "delivery_result_unknown", outbox.ErrorCode)
	require.NoError(t, db.First(&legacy, legacy.ID).Error)
	require.Equal(t, model.AssistantOutboxStatusUnknown, legacy.Status)
}
