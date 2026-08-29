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
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/assistant/builtin"
	"github.com/01121531/subandnew-api/service/assistant/channelservice"
	"github.com/01121531/subandnew-api/service/assistant/provider"
	"github.com/01121531/subandnew-api/service/assistant/runner"
	"github.com/01121531/subandnew-api/service/assistant/secrets"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSystemPromptIsStableAndUsesRuntimeContextTool(t *testing.T) {
	prompt := systemPrompt()
	require.Contains(t, prompt, `instance_scope="all"`)
	require.Contains(t, prompt, "Asia/Shanghai")
	require.Contains(t, prompt, "禁止再次增加或扣减 8 小时")
	require.Contains(t, prompt, "get_runtime_context")
	require.Contains(t, prompt, "get_metric_history")
	require.Contains(t, prompt, "query_usage_records")
	require.NotContains(t, prompt, "当前中国标准时间：")
	require.Equal(t, prompt, systemPrompt())
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
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "list_instances", Arguments: json.RawMessage(`{}`)}}}, Usage: provider.Usage{
			InputTokens: 100, TotalTokens: 100, CachedInputTokens: 80, CacheObservedInputTokens: 100,
		}},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "共 1 个实例，prod 状态正常；数据来自控制平面快照。"}, Usage: provider.Usage{
			InputTokens: 120, OutputTokens: 20, TotalTokens: 140, CachedInputTokens: 90, CacheObservedInputTokens: 120,
		}},
	}}
	processor, err := New(db, cipher, channels,
		func(context.Context) (provider.Client, *model.AssistantModelProfile, error) {
			return modelClient, &model.AssistantModelProfile{Id: 1, Model: "test-model", TimeoutSeconds: 75, RunTimeoutSeconds: 300, MaxOutputTokens: 1024}, nil
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
	require.EqualValues(t, 300, run.DeadlineAt-run.StartedAt)
	require.Equal(t, 75, run.RequestTimeoutSeconds)
	require.Equal(t, 2, run.ModelRequestCount)
	require.EqualValues(t, 170, run.CachedInputTokens)
	require.EqualValues(t, 220, run.CacheObservedInputTokens)
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
	require.Equal(t, "联系 ops@example.com", safeAssistantAnswer("联系 ops@example.com"))
	require.Contains(t, safeAssistantAnswer("访问 https://internal.example"), "不适合")
	require.Contains(t, safeAssistantAnswer("节点 127.0.0.1:8080"), "不适合")
	require.Contains(t, safeAssistantAnswer("password=top-secret-value"), "不适合")
	bounded := safeAssistantAnswer(strings.Repeat("数", 5000))
	require.LessOrEqual(t, len([]rune(bounded)), 4020)
	require.Contains(t, bounded, "已截断")
}

func TestProgressReporterDelaysMessagesAndReportsToolStage(t *testing.T) {
	messages := make(chan string, 4)
	reporter := newProgressReporter(progressReporterConfig{
		initialDelay: 10 * time.Millisecond,
		heartbeat:    time.Second,
		maxMessages:  8,
		now:          time.Now,
		send: func(_ int, message string) {
			messages <- message
		},
	})
	defer reporter.Stop()

	select {
	case message := <-messages:
		require.Equal(t, "已收到，正在分析问题。", message)
	case <-time.After(time.Second):
		t.Fatal("initial progress message was not sent")
	}
	reporter.Report(runner.ProgressEvent{Type: runner.ProgressToolStarted, Tool: "get_metric_history"})
	select {
	case message := <-messages:
		require.Equal(t, "正在查询历史指标。", message)
	case <-time.After(time.Second):
		t.Fatal("tool progress message was not sent")
	}
}

func TestProgressReporterStopsBeforeInitialDelay(t *testing.T) {
	messages := make(chan string, 1)
	reporter := newProgressReporter(progressReporterConfig{
		initialDelay: time.Second,
		heartbeat:    time.Second,
		maxMessages:  8,
		now:          time.Now,
		send:         func(_ int, message string) { messages <- message },
	})
	reporter.Stop()
	select {
	case message := <-messages:
		t.Fatalf("unexpected progress message: %s", message)
	default:
	}
}

func TestAssistantRunFailureCodeDistinguishesTimeout(t *testing.T) {
	require.Equal(t, "agent_run_timeout", assistantRunFailureCode(context.DeadlineExceeded))
	require.Equal(t, "agent_run_timeout", assistantRunFailureCode(runner.ErrModelRequestTimeout))
	require.Equal(t, "agent_run_failed", assistantRunFailureCode(errors.New("provider unavailable")))
}

func TestDescribeAssistantFailureClassifiesProviderAndTimeout(t *testing.T) {
	providerFailure := describeAssistantFailure(assistantRunFailure{
		Code: "agent_run_failed",
		Cause: &runner.RunError{Stage: runner.ErrorStageModelRequest, Cause: &provider.HTTPError{
			StatusCode: http.StatusBadGateway, Code: "origin_bad_gateway", Message: "origin returned an incomplete response",
		}},
	})
	require.Equal(t, runner.ErrorStageModelRequest, providerFailure.Stage)
	require.Equal(t, "provider_unavailable", providerFailure.ReasonCode)
	require.Equal(t, http.StatusBadGateway, providerFailure.ProviderStatusCode)
	require.Equal(t, "origin_bad_gateway", providerFailure.ProviderErrorCode)
	require.Contains(t, providerFailure.Detail, "origin returned an incomplete response")

	timeoutFailure := describeAssistantFailure(assistantRunFailure{
		Code: "agent_run_timeout", Cause: &runner.RunError{Stage: runner.ErrorStageModelStream, Cause: context.DeadlineExceeded},
	})
	require.Equal(t, runner.ErrorStageModelStream, timeoutFailure.Stage)
	require.Equal(t, "run_timeout", timeoutFailure.ReasonCode)
	requestTimeoutFailure := describeAssistantFailure(assistantRunFailure{
		Code: "agent_run_timeout", Cause: &runner.RunError{Stage: runner.ErrorStageModelRequest, Cause: runner.ErrModelRequestTimeout},
	})
	require.Equal(t, "provider_timeout", requestTimeoutFailure.ReasonCode)

	for _, test := range []struct {
		status int
		reason string
	}{
		{status: http.StatusUnauthorized, reason: "provider_authentication_failed"},
		{status: http.StatusTooManyRequests, reason: "provider_rate_limited"},
		{status: http.StatusBadRequest, reason: "provider_rejected_request"},
	} {
		details := describeAssistantFailure(assistantRunFailure{Cause: &provider.HTTPError{StatusCode: test.status}})
		require.Equal(t, test.reason, details.ReasonCode)
	}

	streamFailure := describeAssistantFailure(assistantRunFailure{Cause: &runner.RunError{
		Stage: runner.ErrorStageModelStream, Cause: errors.New("assistant model stream ended before completion"),
	}})
	require.Equal(t, "provider_stream_error", streamFailure.ReasonCode)
	invalidResponse := describeAssistantFailure(assistantRunFailure{Cause: &runner.RunError{
		Stage: runner.ErrorStageModelResponse, Cause: errors.New("assistant model returned an invalid tool call"),
	}})
	require.Equal(t, "invalid_model_response", invalidResponse.ReasonCode)

	oversizedFailure := describeAssistantFailure(assistantRunFailure{Cause: errors.New(strings.Repeat("错", maxAuditErrorDetailBytes))})
	require.True(t, oversizedFailure.DetailTruncated)
	require.LessOrEqual(t, len(oversizedFailure.Detail), maxAuditErrorDetailBytes)
}

func TestFailRunPersistsPartialUsageAndToolTrace(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.AssistantRun{}, &model.AssistantToolCall{}, &model.AssistantInboundEvent{}))
	now := time.Date(2026, 8, 29, 11, 0, 0, 0, time.Local)
	processor := &Processor{db: db, now: func() time.Time { return now }}
	event := model.AssistantInboundEvent{ChannelID: 1, ExternalMessageID: "message", Status: model.AssistantInboundStatusProcessing}
	require.NoError(t, db.Create(&event).Error)
	run := model.AssistantRun{RunID: "failed-run", Model: "model", PromptVersion: "v1", Status: model.AssistantRunStatusRunning, TraceID: "trace", StartedAt: now.Add(-time.Minute).Unix()}
	require.NoError(t, db.Create(&run).Error)
	outcome := runner.Outcome{
		Usage:         provider.Usage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120, CachedInputTokens: 80, CacheObservedInputTokens: 100},
		ToolTraces:    []runner.ToolTrace{{Name: "query", ArgumentsHash: strings.Repeat("a", 64), Error: "tool_execution_failed", ErrorDetail: "upstream tool failure"}},
		ModelRequests: 2, ProviderRetries: 1, RetriedBeforeFirstByte: true,
	}
	cause := &runner.RunError{Stage: runner.ErrorStageToolExecution, Tool: "query", Cause: errors.New("upstream tool failure")}
	err = processor.failRunAndEvent(t.Context(), &event, &run, assistantRunFailure{
		Code: "agent_run_failed", Cause: cause, Outcome: &outcome,
		Specs:   []tool.ToolSpec{{Name: "query", Permission: tool.Permission{Resource: "managed_instance", Action: "usage_view"}, Risk: tool.RiskMedium}},
		Started: now.Add(-time.Minute),
	})
	require.NoError(t, err)
	require.NoError(t, db.First(&run, run.ID).Error)
	require.Equal(t, model.AssistantRunStatusFailed, run.Status)
	require.EqualValues(t, 120, run.TotalTokens)
	require.Equal(t, 2, run.ModelRequestCount)
	require.Equal(t, 1, run.ProviderRetryCount)
	require.True(t, run.RetriedBeforeFirstByte)
	require.Equal(t, "tool_execution_failed", run.ErrorReasonCode)
	require.Equal(t, "upstream tool failure", run.ErrorDetail)
	var call model.AssistantToolCall
	require.NoError(t, db.Where("run_id = ?", run.ID).First(&call).Error)
	require.Equal(t, "upstream tool failure", call.ErrorDetail)
	require.NoError(t, db.First(&event, event.ID).Error)
	require.Equal(t, model.AssistantInboundStatusSucceeded, event.Status)

	err = processor.failRunAndEvent(t.Context(), &event, &run, assistantRunFailure{
		Code: "agent_run_failed", Cause: cause, Outcome: &outcome,
		Specs:   []tool.ToolSpec{{Name: "query", Permission: tool.Permission{Resource: "managed_instance", Action: "usage_view"}, Risk: tool.RiskMedium}},
		Started: now.Add(-time.Minute),
	})
	require.NoError(t, err)
	var callCount int64
	require.NoError(t, db.Model(&model.AssistantToolCall{}).Where("run_id = ?", run.ID).Count(&callCount).Error)
	require.EqualValues(t, 1, callCount)
}
