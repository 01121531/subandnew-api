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
