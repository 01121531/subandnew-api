package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxProviderResponseBytes = 4 << 20

type OpenAICompatibleConfig struct {
	BaseURL             string
	APIKey              string
	Model               string
	HTTPClient          *http.Client
	SafeRetryAfterWrite bool
}

type OpenAICompatibleClient struct {
	endpoint            string
	apiKey              string
	model               string
	httpClient          *http.Client
	safeRetryAfterWrite bool
}

type HTTPError struct {
	StatusCode int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "assistant model request failed"
	}
	if e.Code != "" {
		return fmt.Sprintf("assistant model request failed with status %d (%s)", e.StatusCode, e.Code)
	}
	return fmt.Sprintf("assistant model request failed with status %d", e.StatusCode)
}

func (e *HTTPError) Retriable() bool {
	return e != nil && (e.StatusCode == http.StatusRequestTimeout || e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500)
}

func NewOpenAICompatibleClient(config OpenAICompatibleConfig) (*OpenAICompatibleClient, error) {
	baseURL, err := url.Parse(strings.TrimSpace(config.BaseURL))
	if err != nil || baseURL.Scheme == "" {
		return nil, errors.New("assistant model base URL is invalid")
	}
	if baseURL.Scheme != "https" && baseURL.Scheme != "http" {
		return nil, errors.New("assistant model base URL must use http or https")
	}
	if baseURL.Host == "" {
		return nil, errors.New("assistant model base URL is invalid")
	}
	if baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, errors.New("assistant model base URL must not contain credentials, query, or fragment")
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, errors.New("assistant model name is required")
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	baseURL.Path = chatCompletionsPath(baseURL.Path)
	return &OpenAICompatibleClient{
		endpoint:            baseURL.String(),
		apiKey:              strings.TrimSpace(config.APIKey),
		model:               model,
		httpClient:          client,
		safeRetryAfterWrite: config.SafeRetryAfterWrite,
	}, nil
}

func chatCompletionsPath(basePath string) string {
	normalized := path.Clean("/" + strings.TrimSpace(basePath))
	if strings.HasSuffix(normalized, "/chat/completions") {
		return normalized
	}
	return path.Join(normalized, "chat/completions")
}

func (c *OpenAICompatibleClient) Generate(ctx context.Context, request Request) (Response, error) {
	if err := c.validateRequest(request); err != nil {
		return Response{}, err
	}
	return c.generateWithRetry(ctx, func() (Response, bool, error) {
		return c.generateJSON(ctx, request)
	})
}

func (c *OpenAICompatibleClient) GenerateStream(ctx context.Context, request Request) (Response, error) {
	if err := c.validateRequest(request); err != nil {
		return Response{}, err
	}
	return c.generateWithRetry(ctx, func() (Response, bool, error) {
		return c.generateStreamAttempt(ctx, request)
	})
}

func (c *OpenAICompatibleClient) generateStreamAttempt(ctx context.Context, request Request) (Response, bool, error) {
	response, received, err := c.generateSSE(ctx, request, true)
	if err == nil || received || !streamCompatibilityError(err) {
		return response, received, err
	}
	response, received, err = c.generateSSE(ctx, request, false)
	if err == nil || received || !streamCompatibilityError(err) {
		return response, received, err
	}
	return c.generateJSON(ctx, request)
}

func (c *OpenAICompatibleClient) generateJSON(ctx context.Context, request Request) (Response, bool, error) {
	payload, err := c.requestPayload(request)
	if err != nil {
		return Response{}, false, err
	}
	response, err := c.doRequest(ctx, payload)
	if err != nil {
		return Response{}, false, err
	}
	defer response.Body.Close()
	responseBody, err := readLimitedBody(response.Body)
	if err != nil {
		return Response{}, response.StatusCode >= 200 && response.StatusCode < 300, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Response{}, false, decodeHTTPErrorWithHeader(response.StatusCode, responseBody, response.Header)
	}
	decoded, err := decodeOpenAIResponse(responseBody)
	return decoded, true, err
}

func (c *OpenAICompatibleClient) generateWithRetry(ctx context.Context, attempt func() (Response, bool, error)) (Response, error) {
	response, received, err := attempt()
	if err == nil {
		response.Attempts = 1
		return response, nil
	}
	if received || !c.retriableBeforeFirstByte(ctx, err) {
		return Response{}, &RequestError{Cause: err, Attempts: 1, PartialResponse: response}
	}
	if err := waitForRetry(ctx, retryDelay(err)); err != nil {
		return Response{}, &RequestError{Cause: err, Attempts: 1, PartialResponse: response}
	}
	response, _, err = attempt()
	if err != nil {
		return Response{}, &RequestError{Cause: err, Attempts: 2, RetriedBeforeFirstByte: true, PartialResponse: response}
	}
	response.Attempts = 2
	response.RetriedBeforeFirstByte = true
	return response, nil
}

func (c *OpenAICompatibleClient) retriableBeforeFirstByte(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		return c.safeRetryAfterWrite && httpError.Retriable()
	}
	var transportError *requestTransportError
	if errors.As(err, &transportError) {
		return !transportError.wroteRequest
	}
	var networkError net.Error
	return c.safeRetryAfterWrite && errors.As(err, &networkError)
}

type requestTransportError struct {
	cause        error
	wroteRequest bool
}

func (e *requestTransportError) Error() string { return e.cause.Error() }
func (e *requestTransportError) Unwrap() error { return e.cause }

func retryDelay(err error) time.Duration {
	var httpError *HTTPError
	if errors.As(err, &httpError) && httpError.RetryAfter > 0 {
		return min(httpError.RetryAfter, 15*time.Second)
	}
	return time.Second
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *OpenAICompatibleClient) validateRequest(request Request) error {
	if c == nil || c.httpClient == nil {
		return errors.New("assistant model client is nil")
	}
	if len(request.Messages) == 0 {
		return fmt.Errorf("%w: at least one message is required", ErrInvalidRequest)
	}
	for _, message := range request.Messages {
		if !validRole(message.Role) {
			return fmt.Errorf("%w: unsupported message role %q", ErrInvalidRequest, message.Role)
		}
	}
	for _, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" || len(tool.InputSchema) == 0 || !json.Valid(tool.InputSchema) {
			return fmt.Errorf("%w: tool name and valid schema are required", ErrInvalidRequest)
		}
	}
	return nil
}

func (c *OpenAICompatibleClient) requestPayload(request Request) (openAIRequest, error) {
	payload := openAIRequest{Model: c.model, Messages: make([]openAIMessage, 0, len(request.Messages)), MaxTokens: request.MaxOutputTokens}
	for _, message := range request.Messages {
		payload.Messages = append(payload.Messages, toOpenAIMessage(message))
	}
	for _, tool := range request.Tools {
		payload.Tools = append(payload.Tools, openAITool{Type: "function", Function: openAIFunction{
			Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema,
		}})
	}
	return payload, nil
}

func (c *OpenAICompatibleClient) doRequest(ctx context.Context, payload openAIRequest) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode assistant model request: %w", err)
	}
	wroteRequest := false
	trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { wroteRequest = true }}
	httpRequest, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create assistant model request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return nil, &requestTransportError{cause: fmt.Errorf("send assistant model request: %w", err), wroteRequest: wroteRequest}
	}
	return response, nil
}

func readLimitedBody(body io.Reader) ([]byte, error) {
	responseBody, err := io.ReadAll(io.LimitReader(body, maxProviderResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read assistant model response: %w", err)
	}
	if len(responseBody) > maxProviderResponseBytes {
		return nil, errors.New("assistant model response exceeds size limit")
	}
	return responseBody, nil
}

func decodeOpenAIResponse(responseBody []byte) (Response, error) {
	var decoded openAIResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return Response{}, errors.New("decode assistant model response")
	}
	if len(decoded.Choices) == 0 {
		return Response{}, errors.New("assistant model response has no choices")
	}
	choice := decoded.Choices[0]
	message, err := fromOpenAIMessage(choice.Message)
	if err != nil {
		return Response{}, err
	}
	return Response{
		Message:      message,
		FinishReason: choice.FinishReason,
		Usage:        decoded.Usage.providerUsage(),
	}, nil
}

func (c *OpenAICompatibleClient) generateSSE(ctx context.Context, request Request, includeUsage bool) (Response, bool, error) {
	payload, err := c.requestPayload(request)
	if err != nil {
		return Response{}, false, err
	}
	payload.Stream = true
	if includeUsage {
		payload.StreamOptions = &openAIStreamOptions{IncludeUsage: true}
	}
	response, err := c.doRequest(ctx, payload)
	if err != nil {
		return Response{}, false, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, readErr := readLimitedBody(response.Body)
		if readErr != nil {
			return Response{}, false, readErr
		}
		return Response{}, false, decodeHTTPErrorWithHeader(response.StatusCode, body, response.Header)
	}
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "application/json") {
		body, readErr := readLimitedBody(response.Body)
		if readErr != nil {
			return Response{}, true, readErr
		}
		decoded, decodeErr := decodeOpenAIResponse(body)
		return decoded, true, decodeErr
	}
	return decodeOpenAIStream(response.Body)
}

type streamToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

func decodeOpenAIStream(body io.Reader) (Response, bool, error) {
	limited := &io.LimitedReader{R: body, N: maxProviderResponseBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64*1024), maxProviderResponseBytes)
	result := Response{Message: Message{Role: RoleAssistant}}
	toolCalls := map[int]*streamToolCall{}
	received := false
	done := false
	dataLines := make([]string, 0, 1)
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		if strings.TrimSpace(data) == "[DONE]" {
			done = true
			return nil
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return errors.New("decode assistant model stream")
		}
		received = true
		if chunk.Usage != nil {
			result.Usage = chunk.Usage.providerUsage()
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Role != "" {
				result.Message.Role = choice.Delta.Role
			}
			result.Message.Content += choice.Delta.Content
			if choice.FinishReason != "" {
				result.FinishReason = choice.FinishReason
			}
			for _, delta := range choice.Delta.ToolCalls {
				call := toolCalls[delta.Index]
				if call == nil {
					call = &streamToolCall{}
					toolCalls[delta.Index] = call
				}
				if delta.ID != "" {
					call.id = delta.ID
				}
				call.name += delta.Function.Name
				call.arguments.WriteString(delta.Function.Arguments)
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return result, received, err
			}
			if done {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		if limited.N <= 0 || strings.Contains(strings.ToLower(err.Error()), "token too long") {
			return result, received, errors.New("assistant model response exceeds size limit")
		}
		return result, received, fmt.Errorf("read assistant model stream: %w", err)
	}
	if limited.N <= 0 {
		return result, received, errors.New("assistant model response exceeds size limit")
	}
	if !done {
		if err := flush(); err != nil {
			return result, received, err
		}
		if result.FinishReason == "" {
			return result, received, errors.New("assistant model stream ended before completion")
		}
	}
	if !received {
		return Response{}, false, errors.New("assistant model stream has no events")
	}
	indexes := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		call := toolCalls[index]
		arguments := call.arguments.String()
		if strings.TrimSpace(call.id) == "" || strings.TrimSpace(call.name) == "" || !json.Valid([]byte(arguments)) {
			return result, true, errors.New("assistant model returned an invalid tool call")
		}
		result.Message.ToolCalls = append(result.Message.ToolCalls, ToolCall{ID: call.id, Name: call.name, Arguments: json.RawMessage(arguments)})
	}
	return result, true, nil
}

func streamCompatibilityError(err error) bool {
	var httpError *HTTPError
	if !errors.As(err, &httpError) {
		return false
	}
	if httpError.StatusCode == http.StatusUnsupportedMediaType {
		return true
	}
	if httpError.StatusCode != http.StatusBadRequest && httpError.StatusCode != http.StatusUnprocessableEntity {
		return false
	}
	detail := strings.ToLower(httpError.Code + " " + httpError.Message)
	return strings.Contains(detail, "stream") || strings.Contains(detail, "include_usage") || strings.Contains(detail, "unsupported")
}

func validRole(role string) bool {
	switch role {
	case RoleSystem, RoleUser, RoleAssistant, RoleTool:
		return true
	default:
		return false
	}
}

type openAIRequest struct {
	Model         string               `json:"model"`
	Messages      []openAIMessage      `json:"messages"`
	Tools         []openAITool         `json:"tools,omitempty"`
	MaxTokens     int                  `json:"max_tokens,omitempty"`
	Stream        bool                 `json:"stream,omitempty"`
	StreamOptions *openAIStreamOptions `json:"stream_options,omitempty"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIResponse struct {
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage openAIUsage `json:"usage"`
}

type openAIStreamChunk struct {
	Choices []struct {
		Delta        openAIMessage `json:"delta"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage,omitempty"`
}

type openAIUsage struct {
	PromptTokens       int `json:"prompt_tokens"`
	CompletionTokens   int `json:"completion_tokens"`
	TotalTokens        int `json:"total_tokens"`
	PromptTokenDetails *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
	PromptCacheHitTokens  *int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens *int `json:"prompt_cache_miss_tokens,omitempty"`
}

func (usage openAIUsage) providerUsage() Usage {
	result := Usage{InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens}
	if usage.PromptTokenDetails != nil && usage.PromptTokenDetails.CachedTokens != nil {
		result.CachedInputTokens = max(0, *usage.PromptTokenDetails.CachedTokens)
		result.CacheObservedInputTokens = max(0, usage.PromptTokens)
		return result
	}
	if usage.PromptCacheHitTokens != nil || usage.PromptCacheMissTokens != nil {
		hit, miss := 0, 0
		if usage.PromptCacheHitTokens != nil {
			hit = max(0, *usage.PromptCacheHitTokens)
		}
		if usage.PromptCacheMissTokens != nil {
			miss = max(0, *usage.PromptCacheMissTokens)
		}
		result.CachedInputTokens = hit
		result.CacheObservedInputTokens = hit + miss
		if result.CacheObservedInputTokens == 0 && usage.PromptTokens > 0 {
			result.CacheObservedInputTokens = usage.PromptTokens
		}
	}
	return result
}

func toOpenAIMessage(message Message) openAIMessage {
	converted := openAIMessage{Role: message.Role, Content: message.Content, Name: message.Name, ToolCallID: message.ToolCallID}
	for _, call := range message.ToolCalls {
		toolCall := openAIToolCall{ID: call.ID, Type: "function"}
		toolCall.Function.Name = call.Name
		toolCall.Function.Arguments = string(call.Arguments)
		converted.ToolCalls = append(converted.ToolCalls, toolCall)
	}
	return converted
}

func fromOpenAIMessage(message openAIMessage) (Message, error) {
	converted := Message{Role: message.Role, Content: message.Content, Name: message.Name, ToolCallID: message.ToolCallID}
	for _, call := range message.ToolCalls {
		if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Function.Name) == "" || !json.Valid([]byte(call.Function.Arguments)) {
			return Message{}, errors.New("assistant model returned an invalid tool call")
		}
		converted.ToolCalls = append(converted.ToolCalls, ToolCall{
			ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments),
		})
	}
	return converted, nil
}

func decodeHTTPError(statusCode int, body []byte) error {
	return decodeHTTPErrorWithHeader(statusCode, body, nil)
}

func decodeHTTPErrorWithHeader(statusCode int, body []byte, header http.Header) error {
	var payload struct {
		Error struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Code      any    `json:"code"`
		ErrorCode any    `json:"error_code"`
		ErrorName string `json:"error_name"`
		Message   string `json:"message"`
		Detail    string `json:"detail"`
		Title     string `json:"title"`
	}
	_ = json.Unmarshal(body, &payload)
	code := providerErrorCode(payload.Error.Code)
	if code == "" {
		code = strings.TrimSpace(payload.ErrorName)
	}
	if code == "" {
		code = providerErrorCode(payload.ErrorCode)
	}
	if code == "" {
		code = providerErrorCode(payload.Code)
	}
	message := strings.TrimSpace(payload.Error.Message)
	if message == "" {
		message = strings.TrimSpace(payload.Message)
	}
	if message == "" {
		message = strings.TrimSpace(payload.Detail)
	}
	if message == "" {
		message = strings.TrimSpace(payload.Title)
	}
	return &HTTPError{StatusCode: statusCode, Code: code, Message: message, RetryAfter: parseRetryAfter(header)}
}

func parseRetryAfter(header http.Header) time.Duration {
	if header == nil {
		return 0
	}
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil {
		return max(0, time.Until(retryAt))
	}
	return 0
}

func providerErrorCode(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return strconv.FormatInt(int64(value), 10)
	default:
		return ""
	}
}
