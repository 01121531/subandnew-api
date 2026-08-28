package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	assistantaccess "github.com/01121531/subandnew-api/service/assistant/access"
	"github.com/01121531/subandnew-api/service/assistant/builtin"
	"github.com/01121531/subandnew-api/service/assistant/channelservice"
	"github.com/01121531/subandnew-api/service/assistant/provider"
	"github.com/01121531/subandnew-api/service/assistant/secrets"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSystemPromptDescribesEffectiveDefaultAndFallback(t *testing.T) {
	prompt := systemPrompt(assistantaccess.InstanceResolution{
		DefaultID: 7, DefaultName: "primary", Source: assistantaccess.DefaultSourceGlobal,
		Fallback: true,
	})
	require.Contains(t, prompt, "primary（#7）")
	require.Contains(t, prompt, `instance_scope="all"`)
	require.Contains(t, prompt, "Asia/Shanghai")
	require.Contains(t, prompt, "禁止再次增加或扣减 8 小时")
	require.Contains(t, prompt, "当前中国标准时间")
	require.Contains(t, prompt, "get_metric_history")
	require.Contains(t, prompt, "默认实例失效")
}

type sequenceClient struct {
	mu        sync.Mutex
	responses []provider.Response
	requests  []provider.Request
}

func (client *sequenceClient) Generate(_ context.Context, request provider.Request) (provider.Response, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.requests = append(client.requests, request)
	response := client.responses[0]
	client.responses = client.responses[1:]
	return response, nil
}

func TestProcessorRunsGroundedToolAndDeliversEncryptedOutbox(t *testing.T) {
	var deliveredText string
	var typingStatuses []int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/ilink/bot/get_bot_qrcode":
			_, _ = response.Write([]byte(`{"qrcode":"qr","qrcode_img_content":"image"}`))
		case "/ilink/bot/get_qrcode_status":
			_, _ = response.Write([]byte(`{"status":"confirmed","bot_token":"bot-token","ilink_bot_id":"bot-1","ilink_user_id":"owner"}`))
		case "/ilink/bot/getupdates":
			_, _ = response.Write([]byte(`{"msgs":[{"seq":1,"message_id":101,"from_user_id":"wx-user","message_type":1,"context_token":"context-token","item_list":[{"type":1,"text_item":{"text":"列出实例"}}]}],"get_updates_buf":"cursor-1"}`))
		case "/ilink/bot/getconfig":
			_, _ = response.Write([]byte(`{"typing_ticket":"ticket-1"}`))
		case "/ilink/bot/sendtyping":
			var payload struct {
				Status int `json:"status"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
			typingStatuses = append(typingStatuses, payload.Status)
			_, _ = response.Write([]byte(`{}`))
		case "/ilink/bot/sendmessage":
			var payload struct {
				Message struct {
					Items []struct {
						Text struct {
							Text string `json:"text"`
						} `json:"text_item"`
					} `json:"item_list"`
				} `json:"msg"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
			deliveredText = payload.Message.Items[0].Text.Text
			_, _ = response.Write([]byte(`{}`))
		default:
			response.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.ManagedInstance{}, &model.ManagedInstanceCredential{},
		&model.AssistantChannel{}, &model.AssistantChannelSecret{}, &model.AssistantIdentity{},
		&model.AssistantIdentityInstanceScope{}, &model.AssistantSetting{}, &model.AssistantBindingCode{}, &model.AssistantInboundEvent{},
		&model.AssistantConversation{}, &model.AssistantMessage{}, &model.AssistantRun{}, &model.AssistantToolCall{}, &model.AssistantOutbox{},
	))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	cipher, err := secrets.New(map[string][]byte{"v1": bytes.Repeat([]byte{8}, 32)}, "v1")
	require.NoError(t, err)
	channels, err := channelservice.NewService(db, cipher, channelservice.Config{BaseURL: server.URL, HTTPClient: server.Client()})
	require.NoError(t, err)
	login, err := channels.StartLogin(t.Context(), 1)
	require.NoError(t, err)
	_, err = channels.CheckLogin(t.Context(), login.ChannelID, "")
	require.NoError(t, err)

	user := model.User{Username: "root", Password: "hash", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.AssistantIdentity{
		ChannelID: login.ChannelID, ExternalUserID: "wx-user", UserID: user.Id,
		Status: model.AssistantIdentityStatusActive, AllowedInstanceScope: model.AssistantInstanceScopeAll,
	}).Error)
	require.NoError(t, db.Create(&model.ManagedInstance{
		Name: "prod", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://prod.example",
		Environment: "production", Status: model.ManagedInstanceStatusHealthy,
	}).Error)
	eventIDs, err := channels.PollOnce(t.Context(), login.ChannelID)
	require.NoError(t, err)
	require.Len(t, eventIDs, 1)

	modelClient := &sequenceClient{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "list_instances", Arguments: json.RawMessage(`{}`)}}}},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "共 1 个实例，prod 状态正常；数据来自控制平面快照。"}},
	}}
	processor, err := New(db, cipher, channels,
		func(context.Context) (provider.Client, *model.AssistantModelProfile, error) {
			return modelClient, &model.AssistantModelProfile{Id: 1, Model: "test-model", TimeoutSeconds: 75, MaxOutputTokens: 1024}, nil
		},
		func() (*tool.Registry, error) { return builtin.NewRegistry(db) },
	)
	require.NoError(t, err)
	require.NoError(t, processor.Process(t.Context(), eventIDs[0]))
	require.Equal(t, []int{1, 2}, typingStatuses)
	require.ErrorIs(t, processor.Process(t.Context(), eventIDs[0]), ErrEventNotPending)

	var event model.AssistantInboundEvent
	require.NoError(t, db.First(&event, eventIDs[0]).Error)
	require.Equal(t, model.AssistantInboundStatusSucceeded, event.Status)
	var run model.AssistantRun
	require.NoError(t, db.First(&run).Error)
	require.Equal(t, model.AssistantRunStatusSucceeded, run.Status)
	require.EqualValues(t, 75, run.DeadlineAt-run.StartedAt)
	require.Len(t, modelClient.requests, 2)
	require.Len(t, modelClient.requests[0].Messages, 2)
	require.Equal(t, provider.RoleUser, modelClient.requests[0].Messages[1].Role)
	var messages []model.AssistantMessage
	require.NoError(t, db.Order("id ASC").Find(&messages).Error)
	require.Len(t, messages, 2)
	require.NotContains(t, messages[0].Content, "列出实例")
	require.NotContains(t, messages[1].Content, "共 1 个实例")
	history, err := processor.loadConversationHistory(t.Context(), run.ConversationID)
	require.NoError(t, err)
	require.Equal(t, []provider.Message{{Role: provider.RoleUser, Content: "列出实例"}, {Role: provider.RoleAssistant, Content: "共 1 个实例，prod 状态正常；数据来自控制平面快照。"}}, history)
	var outbox model.AssistantOutbox
	require.NoError(t, db.First(&outbox).Error)
	require.NotContains(t, outbox.Payload, "共 1 个实例")
	require.NotContains(t, outbox.Payload, "context-token")
	require.NoError(t, processor.Deliver(t.Context(), outbox.ID))
	require.Contains(t, deliveredText, "共 1 个实例")
	require.NoError(t, db.First(&outbox, outbox.ID).Error)
	require.Equal(t, model.AssistantOutboxStatusSucceeded, outbox.Status)

	exhaustedEvent := model.AssistantInboundEvent{
		ChannelID: login.ChannelID, AccountID: "bot-1", ExternalMessageID: "exhausted-event",
		PeerID: "wx-user", ExternalUserID: "wx-user", Payload: "unreadable", Status: model.AssistantInboundStatusFailed, Attempt: maxInboundAttempts,
	}
	require.NoError(t, db.Create(&exhaustedEvent).Error)
	require.ErrorIs(t, processor.Process(t.Context(), exhaustedEvent.ID), ErrEventNotPending)
	exhaustedOutbox := model.AssistantOutbox{
		ChannelID: login.ChannelID, ReplyKey: "exhausted-outbox", Payload: "unreadable",
		Status: model.AssistantOutboxStatusFailed, Attempt: maxOutboxAttempts,
	}
	require.NoError(t, db.Create(&exhaustedOutbox).Error)
	require.ErrorIs(t, processor.Deliver(t.Context(), exhaustedOutbox.ID), ErrOutboxNotDue)
}

func TestSafeAssistantAnswerBlocksSensitiveOutputAndBoundsLength(t *testing.T) {
	require.Contains(t, safeAssistantAnswer("token: Bearer abcdefghijklmnop"), "不适合")
	require.Contains(t, safeAssistantAnswer("联系 ops@example.com"), "不适合")
	require.Contains(t, safeAssistantAnswer("访问 https://internal.example"), "不适合")
	bounded := safeAssistantAnswer(strings.Repeat("数", 5000))
	require.LessOrEqual(t, len([]rune(bounded)), 4020)
	require.Contains(t, bounded, "已截断")
}

func TestAssistantRunFailureCodeDistinguishesTimeout(t *testing.T) {
	require.Equal(t, "agent_run_timeout", assistantRunFailureCode(context.DeadlineExceeded))
	require.Equal(t, "agent_run_failed", assistantRunFailureCode(errors.New("provider unavailable")))
}
