package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssistantPersistenceDefaults(t *testing.T) {
	truncateTables(t)

	channel := &AssistantChannel{Type: AssistantChannelTypeWechatILink, AccountID: "bot-defaults"}
	require.NoError(t, DB.Create(channel).Error)
	require.Equal(t, AssistantChannelStatusUnbound, channel.Status)
	require.Equal(t, "{}", channel.Config)
	require.Positive(t, channel.CreatedAt)
	require.Equal(t, channel.CreatedAt, channel.UpdatedAt)
	secret := &AssistantChannelSecret{ChannelID: channel.ID, Ciphertext: "ciphertext", KeyVersion: "v1", Fingerprint: "fingerprint"}
	require.NoError(t, DB.Create(secret).Error)
	require.Positive(t, secret.CreatedAt)

	identity := &AssistantIdentity{
		ChannelID: channel.ID, ExternalUserID: "wx-user-defaults", UserID: 101,
	}
	require.NoError(t, DB.Create(identity).Error)
	require.Equal(t, AssistantIdentityStatusPending, identity.Status)
	require.Equal(t, AssistantInstanceScopeNone, identity.AllowedInstanceScope)
	require.Positive(t, identity.CreatedAt)

	scope := &AssistantIdentityInstanceScope{IdentityID: identity.ID, InstanceID: 501}
	require.NoError(t, DB.Create(scope).Error)
	require.Positive(t, scope.CreatedAt)

	inbound := &AssistantInboundEvent{
		ChannelID: channel.ID, AccountID: channel.AccountID,
		ExternalMessageID: "message-defaults", PeerID: "peer-defaults",
		ExternalUserID: identity.ExternalUserID, Payload: `{}`,
	}
	require.NoError(t, DB.Create(inbound).Error)
	require.Equal(t, AssistantInboundStatusPending, inbound.Status)
	require.Positive(t, inbound.ReceivedAt)
	require.Positive(t, inbound.CreatedAt)

	conversation := &AssistantConversation{
		ChannelID: channel.ID, AccountID: channel.AccountID,
		PeerID: inbound.PeerID, UserID: identity.UserID,
	}
	require.NoError(t, DB.Create(conversation).Error)
	require.Equal(t, AssistantConversationStatusActive, conversation.Status)
	require.Positive(t, conversation.CreatedAt)
	message := &AssistantMessage{ConversationID: conversation.ID, InboundEventID: inbound.ID, Role: AssistantMessageRoleUser, Content: "ciphertext", ContentKeyVersion: "v1"}
	require.NoError(t, DB.Create(message).Error)
	require.Positive(t, message.CreatedAt)

	run := &AssistantRun{
		RunID: "run-defaults", ConversationID: conversation.ID,
		TriggerMessageID: inbound.ID, Model: "gpt-test", PromptVersion: "v1",
		TraceID: "trace-defaults",
	}
	require.NoError(t, DB.Create(run).Error)
	require.Equal(t, AssistantRunStatusPending, run.Status)
	require.Equal(t, "0", run.Cost)
	require.Positive(t, run.CreatedAt)

	call := &AssistantToolCall{
		RunID: run.ID, Sequence: 1, Tool: "list_instances",
		ArgumentsRedacted: `{}`, Permission: "managed_instance.view",
	}
	require.NoError(t, DB.Create(call).Error)
	require.Equal(t, AssistantToolCallStatusPending, call.Status)
	require.Equal(t, AssistantToolRiskLow, call.Risk)
	require.Positive(t, call.CreatedAt)

	outbox := &AssistantOutbox{
		ChannelID: channel.ID, ConversationID: conversation.ID, RunID: run.ID,
		ReplyKey: "run-defaults:0", Payload: `{}`,
	}
	require.NoError(t, DB.Create(outbox).Error)
	require.Equal(t, AssistantOutboxStatusPending, outbox.Status)
	require.Positive(t, outbox.CreatedAt)
}

func TestAssistantPersistenceUniqueKeys(t *testing.T) {
	truncateTables(t)

	channel := &AssistantChannel{Type: AssistantChannelTypeWechatILink, AccountID: "bot-unique"}
	require.NoError(t, DB.Create(channel).Error)
	require.Error(t, DB.Create(&AssistantChannel{
		Type: channel.Type, AccountID: channel.AccountID,
	}).Error)

	identity := &AssistantIdentity{ChannelID: channel.ID, ExternalUserID: "wx-user-unique", UserID: 102}
	require.NoError(t, DB.Create(identity).Error)
	require.Error(t, DB.Create(&AssistantIdentity{
		ChannelID: channel.ID, ExternalUserID: identity.ExternalUserID, UserID: 103,
	}).Error)

	scope := &AssistantIdentityInstanceScope{IdentityID: identity.ID, InstanceID: 502}
	require.NoError(t, DB.Create(scope).Error)
	require.Error(t, DB.Create(&AssistantIdentityInstanceScope{
		IdentityID: identity.ID, InstanceID: scope.InstanceID,
	}).Error)

	inbound := &AssistantInboundEvent{
		ChannelID: channel.ID, AccountID: channel.AccountID,
		ExternalMessageID: "message-unique", PeerID: "peer-unique",
		ExternalUserID: identity.ExternalUserID, Payload: `{}`,
	}
	require.NoError(t, DB.Create(inbound).Error)
	require.Error(t, DB.Create(&AssistantInboundEvent{
		ChannelID: channel.ID, AccountID: channel.AccountID,
		ExternalMessageID: inbound.ExternalMessageID, PeerID: inbound.PeerID,
		ExternalUserID: identity.ExternalUserID, Payload: `{}`,
	}).Error)

	conversation := &AssistantConversation{
		ChannelID: channel.ID, AccountID: channel.AccountID, PeerID: inbound.PeerID, UserID: identity.UserID,
	}
	require.NoError(t, DB.Create(conversation).Error)
	require.Error(t, DB.Create(&AssistantConversation{
		ChannelID: channel.ID, AccountID: channel.AccountID, PeerID: inbound.PeerID, UserID: identity.UserID,
	}).Error)
	message := &AssistantMessage{ConversationID: conversation.ID, InboundEventID: inbound.ID, Role: AssistantMessageRoleUser, Content: "ciphertext", ContentKeyVersion: "v1"}
	require.NoError(t, DB.Create(message).Error)
	require.Error(t, DB.Create(&AssistantMessage{ConversationID: conversation.ID, InboundEventID: inbound.ID, Role: AssistantMessageRoleUser, Content: "other", ContentKeyVersion: "v1"}).Error)

	run := &AssistantRun{
		RunID: "run-unique", ConversationID: conversation.ID,
		TriggerMessageID: inbound.ID, Model: "gpt-test", PromptVersion: "v1", TraceID: "trace-unique",
	}
	require.NoError(t, DB.Create(run).Error)
	require.Error(t, DB.Create(&AssistantRun{
		RunID: run.RunID, ConversationID: conversation.ID,
		TriggerMessageID: inbound.ID, Model: "gpt-test", PromptVersion: "v1", TraceID: "trace-duplicate",
	}).Error)

	call := &AssistantToolCall{
		RunID: run.ID, Sequence: 1, Tool: "list_instances",
		ArgumentsRedacted: `{}`, Permission: "managed_instance.view",
	}
	require.NoError(t, DB.Create(call).Error)
	require.Error(t, DB.Create(&AssistantToolCall{
		RunID: run.ID, Sequence: call.Sequence, Tool: "get_instance_overview",
		ArgumentsRedacted: `{}`, Permission: "managed_instance.view",
	}).Error)

	outbox := &AssistantOutbox{
		ChannelID: channel.ID, ConversationID: conversation.ID, RunID: run.ID,
		ReplyKey: "run-unique:0", Payload: `{}`,
	}
	require.NoError(t, DB.Create(outbox).Error)
	require.Error(t, DB.Create(&AssistantOutbox{
		ChannelID: channel.ID, ConversationID: conversation.ID, RunID: run.ID,
		ReplyKey: outbox.ReplyKey, Payload: `{}`,
	}).Error)
}

func TestAssistantPersistenceJSONHidesSensitiveContent(t *testing.T) {
	encoded, err := json.Marshal(struct {
		Channel       AssistantChannel       `json:"channel"`
		ChannelSecret AssistantChannelSecret `json:"channel_secret"`
		Inbound       AssistantInboundEvent  `json:"inbound"`
		Conversation  AssistantConversation  `json:"conversation"`
		Message       AssistantMessage       `json:"message"`
		Outbox        AssistantOutbox        `json:"outbox"`
	}{
		Channel:       AssistantChannel{Config: "channel-secret-marker"},
		ChannelSecret: AssistantChannelSecret{Ciphertext: "encrypted-secret-marker", KeyVersion: "secret-version-marker"},
		Inbound:       AssistantInboundEvent{Payload: "inbound-secret-marker", Cursor: "cursor-secret-marker"},
		Conversation:  AssistantConversation{Summary: "summary-secret-marker"},
		Message:       AssistantMessage{Content: "message-secret-marker", ContentKeyVersion: "message-version-marker"},
		Outbox:        AssistantOutbox{Payload: "outbox-secret-marker", RemoteResult: "remote-secret-marker"},
	})
	require.NoError(t, err)

	for _, secret := range []string{
		"channel-secret-marker", "inbound-secret-marker", "cursor-secret-marker",
		"encrypted-secret-marker", "secret-version-marker",
		"summary-secret-marker", "outbox-secret-marker", "remote-secret-marker",
		"message-secret-marker", "message-version-marker",
	} {
		require.NotContains(t, string(encoded), secret)
	}
}
