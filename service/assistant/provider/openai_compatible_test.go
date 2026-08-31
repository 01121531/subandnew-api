package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeOpenAIStreamHandlesCRLFAndRejectsIncompleteData(t *testing.T) {
	valid := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\r\n\r\ndata: [DONE]\r\n\r\n"
	result, received, err := decodeOpenAIStream(strings.NewReader(valid))
	require.NoError(t, err)
	require.True(t, received)
	require.Equal(t, "OK", result.Message.Content)

	incomplete := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"}}]}\n\n"
	_, received, err = decodeOpenAIStream(strings.NewReader(incomplete))
	require.ErrorContains(t, err, "before completion")
	require.True(t, received)

	_, _, err = decodeOpenAIStream(strings.NewReader("data: " + strings.Repeat("x", maxProviderResponseBytes+1)))
	require.ErrorContains(t, err, "size limit")
}

func TestOpenAICompatibleGenerateStreamTextAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var payload openAIRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.True(t, payload.Stream)
		require.NotNil(t, payload.StreamOptions)
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"你\"}}]}\n\n")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":2,\"total_tokens\":102,\"prompt_tokens_details\":{\"cached_tokens\":80}}}\n\n")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: server.URL, Model: "model", HTTPClient: server.Client()})
	require.NoError(t, err)
	result, err := client.GenerateStream(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	require.NoError(t, err)
	require.Equal(t, "你好", result.Message.Content)
	require.Equal(t, "stop", result.FinishReason)
	require.Equal(t, 80, result.Usage.CachedInputTokens)
	require.Equal(t, 100, result.Usage.CacheObservedInputTokens)
}

func TestOpenAICompatibleGenerateStreamReassemblesToolCallsAndDeepSeekUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"get_\",\"arguments\":\"{\\\"id\\\":\"}}]}}]}\n\n")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"status\",\"arguments\":\"7}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":50,\"completion_tokens\":4,\"total_tokens\":54,\"prompt_cache_hit_tokens\":30,\"prompt_cache_miss_tokens\":20}}\n\n")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: server.URL, Model: "model", HTTPClient: server.Client()})
	require.NoError(t, err)
	result, err := client.GenerateStream(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "status"}}})
	require.NoError(t, err)
	require.Equal(t, ToolCall{ID: "call-1", Name: "get_status", Arguments: json.RawMessage(`{"id":7}`)}, result.Message.ToolCalls[0])
	require.Equal(t, 30, result.Usage.CachedInputTokens)
	require.Equal(t, 50, result.Usage.CacheObservedInputTokens)
}

func TestOpenAICompatibleGenerateStreamFallsBackOnlyBeforeData(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		attempts++
		var payload openAIRequest
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		if payload.Stream {
			response.WriteHeader(http.StatusBadRequest)
			_, _ = response.Write([]byte(`{"error":{"code":"unsupported_stream"}}`))
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"fallback"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer server.Close()

	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: server.URL, Model: "model", HTTPClient: server.Client()})
	require.NoError(t, err)
	result, err := client.GenerateStream(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	require.NoError(t, err)
	require.Equal(t, "fallback", result.Message.Content)
	require.Equal(t, 3, attempts)

	attempts = 0
	partialServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		attempts++
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"}}]}\n\n")
		_, _ = fmt.Fprint(response, "data: not-json\n\n")
	}))
	defer partialServer.Close()
	client, err = NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: partialServer.URL, Model: "model", HTTPClient: partialServer.Client()})
	require.NoError(t, err)
	_, err = client.GenerateStream(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	require.Error(t, err)
	require.Equal(t, 1, attempts)
	requestAttempts, retried := RequestAttempts(err)
	require.Equal(t, 1, requestAttempts)
	require.False(t, retried)
}

func TestOpenAICompatibleRetriesTransientFailureOnlyBeforeFirstData(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			response.WriteHeader(http.StatusBadGateway)
			_, _ = response.Write([]byte(`{"error":{"code":"origin_bad_gateway"}}`))
			return
		}
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"recovered\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = fmt.Fprint(response, "data: [DONE]\n\n")
	}))
	defer server.Close()

	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: server.URL, Model: "model", HTTPClient: server.Client(), SafeRetryAfterWrite: true})
	require.NoError(t, err)
	result, err := client.GenerateStream(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	require.NoError(t, err)
	require.Equal(t, "recovered", result.Message.Content)
	require.Equal(t, 2, result.Attempts)
	require.True(t, result.RetriedBeforeFirstByte)
	require.Equal(t, 2, attempts)
}

func TestOpenAICompatibleDoesNotRetryWrittenRequestByDefault(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		attempts++
		response.WriteHeader(http.StatusBadGateway)
		_, _ = response.Write([]byte(`{"error":{"code":"origin_bad_gateway"}}`))
	}))
	defer server.Close()
	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: server.URL, Model: "model", HTTPClient: server.Client()})
	require.NoError(t, err)
	_, err = client.GenerateStream(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	require.Error(t, err)
	require.Equal(t, 1, attempts)
}

func TestOpenAICompatiblePreservesUsageWhenStreamFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(response, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":40,\"completion_tokens\":2,\"total_tokens\":42}}\n\n")
		_, _ = fmt.Fprint(response, "data: not-json\n\n")
	}))
	defer server.Close()
	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: server.URL, Model: "model", HTTPClient: server.Client()})
	require.NoError(t, err)
	_, err = client.GenerateStream(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	require.Error(t, err)
	partial, ok := PartialResponse(err)
	require.True(t, ok)
	require.Equal(t, 42, partial.Usage.TotalTokens)
}

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

func TestOpenAICompatibleNonStreamingUsageCacheVariants(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11,"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":10}
		}`))
	}))
	defer server.Close()
	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: server.URL, Model: "model", HTTPClient: server.Client()})
	require.NoError(t, err)
	result, err := client.Generate(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	require.NoError(t, err)
	require.Zero(t, result.Usage.CachedInputTokens)
	require.Equal(t, 10, result.Usage.CacheObservedInputTokens)
}

func TestOpenAICompatibleAcceptsFullChatCompletionsURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v1/chat/completions", request.URL.Path)
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer server.Close()

	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{
		BaseURL: server.URL + "/v1/chat/completions/", Model: "test-model", HTTPClient: server.Client(),
	})
	require.NoError(t, err)
	response, err := client.Generate(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "ping"}}})
	require.NoError(t, err)
	require.Equal(t, "OK", response.Message.Content)
}

func TestOpenAICompatibleHTTPErrorIsSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusTooManyRequests)
		_, _ = response.Write([]byte(`{"error":{"code":"rate_limit","message":"request containing secret-value failed"}}`))
	}))
	defer server.Close()
	client, err := NewOpenAICompatibleClient(OpenAICompatibleConfig{BaseURL: server.URL, APIKey: "secret-value", Model: "model", HTTPClient: server.Client(), SafeRetryAfterWrite: true})
	require.NoError(t, err)
	_, err = client.Generate(t.Context(), Request{Messages: []Message{{Role: RoleUser, Content: "hello"}}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-value")
	var httpError *HTTPError
	require.ErrorAs(t, err, &httpError)
	require.True(t, httpError.Retriable())
	require.Equal(t, "rate_limit", httpError.Code)
	attempts, retried := RequestAttempts(err)
	require.Equal(t, 2, attempts)
	require.True(t, retried)
}

func TestDecodeHTTPErrorSupportsProblemDetails(t *testing.T) {
	err := decodeHTTPError(http.StatusBadGateway, []byte(`{
		"title":"Error 502: Bad gateway",
		"detail":"The origin web server returned an invalid response.",
		"error_code":502,
		"error_name":"origin_bad_gateway"
	}`))
	var httpError *HTTPError
	require.ErrorAs(t, err, &httpError)
	require.Equal(t, http.StatusBadGateway, httpError.StatusCode)
	require.Equal(t, "origin_bad_gateway", httpError.Code)
	require.Equal(t, "The origin web server returned an invalid response.", httpError.Message)
	require.NotContains(t, err.Error(), httpError.Message)
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
