package runner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/service/assistant/provider"
	"github.com/01121531/subandnew-api/service/assistant/tool"
	"github.com/stretchr/testify/require"
)

type fakeClient struct {
	responses []provider.Response
	requests  []provider.Request
}

type fakeStreamingClient struct {
	fakeClient
	streamCalls int
}

func (client *fakeStreamingClient) GenerateStream(_ context.Context, request provider.Request) (provider.Response, error) {
	client.streamCalls++
	return client.fakeClient.Generate(context.Background(), request)
}

func (client *fakeClient) Generate(_ context.Context, request provider.Request) (provider.Response, error) {
	client.requests = append(client.requests, request)
	if len(client.responses) == 0 {
		return provider.Response{}, errors.New("no fake response")
	}
	response := client.responses[0]
	client.responses = client.responses[1:]
	return response, nil
}

type listInput struct {
	Limit int `json:"limit"`
}

func (input listInput) Validate() error {
	if input.Limit < 1 || input.Limit > 10 {
		return errors.New("limit out of range")
	}
	return nil
}

func newRunnerRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	registry, err := tool.NewRegistry(func(_ context.Context, request tool.AuthorizationRequest) error {
		if request.Execution.UserID != 7 {
			return errors.New("denied")
		}
		return nil
	})
	require.NoError(t, err)
	err = tool.Register(registry, tool.ToolSpec{
		Name: "list_instances", Version: "v1", Description: "List visible instances",
		Permission: tool.Permission{Resource: "managed_instance", Action: "view"}, Risk: tool.RiskLow,
		ReadOnly: true, Idempotent: true, InputSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}},"required":["limit"]}`),
	}, func(_ context.Context, _ tool.ExecutionContext, input listInput) (tool.Output[map[string]any], error) {
		now := time.Date(2026, 8, 29, 1, 5, 58, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
		return tool.Output[map[string]any]{
			Data:       map[string]any{"count": input.Limit, "observed_at": now.Format(time.RFC3339)},
			Provenance: []tool.Provenance{{Source: "control-plane", ObservedAt: now}},
			Freshness:  tool.Freshness{State: tool.FreshnessSnapshot, ObservedAt: now},
		}, nil
	})
	require.NoError(t, err)
	return registry
}

func TestRunnerExecutesToolAndReturnsGroundedAnswer(t *testing.T) {
	client := &fakeClient{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "list_instances", Arguments: json.RawMessage(`{"limit":5}`)}}}, Usage: provider.Usage{TotalTokens: 10}},
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "共有 5 个实例，数据截至 1970-01-01。"}, Usage: provider.Usage{TotalTokens: 8}},
	}}
	runner, err := New(client, newRunnerRegistry(t), Config{SystemPrompt: "Only use tool data."})
	require.NoError(t, err)
	outcome, err := runner.Run(t.Context(), tool.ExecutionContext{
		RunID: "run-1", ConversationID: "conversation-1", Channel: "wechat", IdentityID: 1, UserID: 7, UserRole: 10,
	}, []provider.Message{{Role: provider.RoleUser, Content: "有多少实例？"}})
	require.NoError(t, err)
	require.Contains(t, outcome.Answer, "5 个实例")
	require.Equal(t, 2, outcome.ProviderSteps)
	require.Equal(t, 1, outcome.ToolCalls)
	require.Equal(t, 18, outcome.Usage.TotalTokens)
	require.Len(t, outcome.ToolTraces[0].ArgumentsHash, 64)
	require.Len(t, outcome.ToolTraces[0].ResultHash, 64)
	require.Len(t, client.requests, 2)
	require.Len(t, client.requests[0].Tools, 1)
	require.Equal(t, provider.RoleTool, client.requests[1].Messages[len(client.requests[1].Messages)-1].Role)
	require.Contains(t, client.requests[1].Messages[len(client.requests[1].Messages)-1].Content, `"freshness"`)
	require.Contains(t, client.requests[1].Messages[len(client.requests[1].Messages)-1].Content, `"observed_at":"2026-08-29T01:05:58+08:00"`)
	require.NotContains(t, client.requests[1].Messages[len(client.requests[1].Messages)-1].Content, `1787936758`)
}

func TestRunnerPrefersStreamingAndAccumulatesCacheUsage(t *testing.T) {
	client := &fakeStreamingClient{fakeClient: fakeClient{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, Content: "done"}, Usage: provider.Usage{
			InputTokens: 100, OutputTokens: 5, TotalTokens: 105, CachedInputTokens: 80, CacheObservedInputTokens: 100,
		}},
	}}}
	runner, err := New(client, newRunnerRegistry(t), Config{SystemPrompt: "stable"})
	require.NoError(t, err)
	outcome, err := runner.Run(t.Context(), tool.ExecutionContext{
		RunID: "run", ConversationID: "conversation", Channel: "wechat", IdentityID: 1, UserID: 7,
	}, []provider.Message{{Role: provider.RoleUser, Content: "hello"}})
	require.NoError(t, err)
	require.Equal(t, 1, client.streamCalls)
	require.Equal(t, 80, outcome.Usage.CachedInputTokens)
	require.Equal(t, 100, outcome.Usage.CacheObservedInputTokens)
}

func TestRunnerFailsClosedOnAuthorization(t *testing.T) {
	client := &fakeClient{responses: []provider.Response{{Message: provider.Message{
		Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "list_instances", Arguments: json.RawMessage(`{"limit":5}`)}},
	}}}}
	runner, err := New(client, newRunnerRegistry(t), Config{SystemPrompt: "safe", MaxSteps: 2})
	require.NoError(t, err)
	outcome, err := runner.Run(t.Context(), tool.ExecutionContext{
		RunID: "run-1", ConversationID: "conversation-1", Channel: "wechat", IdentityID: 1, UserID: 8,
	}, []provider.Message{{Role: provider.RoleUser, Content: "list"}})
	require.ErrorIs(t, err, tool.ErrAuthorizationDenied)
	var runErr *RunError
	require.ErrorAs(t, err, &runErr)
	require.Equal(t, ErrorStageToolExecution, runErr.Stage)
	require.Len(t, outcome.ToolTraces, 1)
	require.Equal(t, "authorization_denied", outcome.ToolTraces[0].Error)
	require.Contains(t, outcome.ToolTraces[0].ErrorDetail, "authorization")
}

func TestBoundedAuditErrorDetailPreservesUTF8AndMarksTruncation(t *testing.T) {
	detail, truncated := boundedAuditErrorDetail(strings.Repeat("错", maxAuditErrorDetailBytes))
	require.True(t, truncated)
	require.True(t, utf8.ValidString(detail))
	require.LessOrEqual(t, len(detail), maxAuditErrorDetailBytes)
}

func TestRunnerRejectsDuplicateToolCallAndStepLimit(t *testing.T) {
	client := &fakeClient{responses: []provider.Response{
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "same", Name: "list_instances", Arguments: json.RawMessage(`{"limit":1}`)}}}},
		{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "same", Name: "list_instances", Arguments: json.RawMessage(`{"limit":1}`)}}}},
	}}
	runner, err := New(client, newRunnerRegistry(t), Config{SystemPrompt: "safe", MaxSteps: 2})
	require.NoError(t, err)
	_, err = runner.Run(t.Context(), tool.ExecutionContext{
		RunID: "run-1", ConversationID: "conversation-1", Channel: "wechat", IdentityID: 1, UserID: 7,
	}, []provider.Message{{Role: provider.RoleUser, Content: "list"}})
	require.ErrorIs(t, err, ErrInvalidModelResponse)

	client = &fakeClient{responses: []provider.Response{{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "one", Name: "list_instances", Arguments: json.RawMessage(`{"limit":1}`)}}}}}}
	runner, err = New(client, newRunnerRegistry(t), Config{SystemPrompt: "safe", MaxSteps: 1})
	require.NoError(t, err)
	_, err = runner.Run(t.Context(), tool.ExecutionContext{
		RunID: "run-1", ConversationID: "conversation-1", Channel: "wechat", IdentityID: 1, UserID: 7,
	}, []provider.Message{{Role: provider.RoleUser, Content: "list"}})
	require.ErrorIs(t, err, ErrStepLimit)
}

func TestRunnerAcceptsConfiguredTimeoutUpToTwoMinutes(t *testing.T) {
	_, err := New(&fakeClient{}, newRunnerRegistry(t), Config{SystemPrompt: "safe", Timeout: 120 * time.Second})
	require.NoError(t, err)

	_, err = New(&fakeClient{}, newRunnerRegistry(t), Config{SystemPrompt: "safe", Timeout: 121 * time.Second})
	require.ErrorIs(t, err, ErrInvalidConfiguration)
}

func TestRunnerOnlyAdvertisesAndExecutesSelectedTools(t *testing.T) {
	client := &fakeClient{responses: []provider.Response{{Message: provider.Message{Role: provider.RoleAssistant, Content: "hello"}}}}
	runner, err := New(client, newRunnerRegistry(t), Config{SystemPrompt: "safe", ToolNames: []string{}})
	require.NoError(t, err)
	_, err = runner.Run(t.Context(), tool.ExecutionContext{
		RunID: "run", ConversationID: "conversation", Channel: "wechat", IdentityID: 1, UserID: 7,
	}, []provider.Message{{Role: provider.RoleUser, Content: "hello"}})
	require.NoError(t, err)
	require.Empty(t, client.requests[0].Tools)

	client = &fakeClient{responses: []provider.Response{{Message: provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call", Name: "list_instances", Arguments: json.RawMessage(`{"limit":1}`)}}}}}}
	runner, err = New(client, newRunnerRegistry(t), Config{SystemPrompt: "safe", ToolNames: []string{}})
	require.NoError(t, err)
	_, err = runner.Run(t.Context(), tool.ExecutionContext{
		RunID: "run", ConversationID: "conversation", Channel: "wechat", IdentityID: 1, UserID: 7,
	}, []provider.Message{{Role: provider.RoleUser, Content: "hello"}})
	require.ErrorIs(t, err, ErrToolNotAllowed)

	_, err = New(client, newRunnerRegistry(t), Config{SystemPrompt: "safe", ToolNames: []string{"missing"}})
	require.ErrorIs(t, err, ErrInvalidConfiguration)
}
