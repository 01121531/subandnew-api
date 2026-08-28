package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAICompatibleGenerateWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/chat/completions", request.URL.Path)
		require.Equal(t, "Bearer test-key", request.Header.Get("Authorization"))
		var payload openAIRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, "test-model", payload.Model)
		require.Len(t, payload.Tools, 1)
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"list_instances","arguments":"{\"limit\":5}"}}]},"finish_reason":"tool_calls"}],
			"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}
		}`))
	}))
	defer server.Close()

	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: server.URL + "/v1", APIKey: "test-key", Model: "test-model", HTTPClient: server.Client()})
	require.NoError(t, err)
	result, err := client.Generate(t.Context(), Request{
		Messages:        []Message{{Role: RoleUser, Content: "列出实例"}},
		Tools:           []ToolDefinition{{Name: "list_instances", Description: "List visible instances", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		MaxOutputTokens: 100,
	})
	require.NoError(t, err)
	require.Equal(t, "tool_calls", result.FinishReason)
	require.Equal(t, ToolCall{ID: "call-1", Name: "list_instances", Arguments: json.RawMessage(`{"limit":5}`)}, result.Message.ToolCalls[0])
	require.Equal(t, 14, result.Usage.TotalTokens)
}

func TestOpenAICompatibleHTTPErrorIsSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTooManyRequests)
		_, _ = response.Write([]byte(`{"error":{"code":"rate_limit","message":"request containing secret-value failed"}}`))
	}))
	defer server.Close()
	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: server.URL, APIKey: "secret-value", Model: "model", HTTPClient: server.Client()})
	require.NoError(t, err)
	_, err = client.Generate(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-value")
	var httpError *HTTPError
	require.ErrorAs(t, err, &httpError)
	require.True(t, httpError.Retriable())
	require.Equal(t, "rate_limit", httpError.Code)
}

func TestOpenAICompatibleValidatesConfigurationAndInput(t *testing.T) {
	_, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: "file:///tmp/model", Model: "model"})
	require.ErrorContains(t, err, "http or https")
	_, err = NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: "https://user:pass@example.com/v1", Model: "model"})
	require.ErrorContains(t, err, "must not contain credentials")
	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: "https://example.com/v1", Model: "model"})
	require.NoError(t, err)
	_, err = client.Generate(t.Context(), Request{})
	require.ErrorIs(t, err, ErrInvalidRequest)
	_, err = client.Generate(t.Context(), Request{Messages: []Message{{Role: "developer", Content: "hello"}}})
	require.ErrorIs(t, err, ErrInvalidRequest)
}
