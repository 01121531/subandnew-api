package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const maxProviderResponseBytes = 4 << 20

type OpenAICompatibleConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

type OpenAICompatibleClient struct {
	endpoint   string
	apiKey     string
	model      string
	httpClient *http.Client
}

type HTTPError struct {
	StatusCode int
	Code       string
	Message    string
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
	baseURL.Path = path.Join(strings.TrimSuffix(baseURL.Path, "/"), "chat/completions")
	return &OpenAICompatibleClient{
		endpoint:   baseURL.String(),
		apiKey:     strings.TrimSpace(config.APIKey),
		model:      model,
		httpClient: client,
	}, nil
}

func (c *OpenAICompatibleClient) Generate(ctx context.Context, request Request) (Response, error) {
	if c == nil || c.httpClient == nil {
		return Response{}, errors.New("assistant model client is nil")
	}
	if len(request.Messages) == 0 {
		return Response{}, fmt.Errorf("%w: at least one message is required", ErrInvalidRequest)
	}
	for _, message := range request.Messages {
		if !validRole(message.Role) {
			return Response{}, fmt.Errorf("%w: unsupported message role %q", ErrInvalidRequest, message.Role)
		}
	}

	payload := openAIRequest{Model: c.model, Messages: make([]openAIMessage, 0, len(request.Messages)), MaxTokens: request.MaxOutputTokens}
	for _, message := range request.Messages {
		payload.Messages = append(payload.Messages, toOpenAIMessage(message))
	}
	for _, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" || len(tool.InputSchema) == 0 || !json.Valid(tool.InputSchema) {
			return Response{}, fmt.Errorf("%w: tool name and valid schema are required", ErrInvalidRequest)
		}
		payload.Tools = append(payload.Tools, openAITool{Type: "function", Function: openAIFunction{
			Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema,
		}})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("encode assistant model request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("create assistant model request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return Response{}, fmt.Errorf("send assistant model request: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxProviderResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return Response{}, fmt.Errorf("read assistant model response: %w", err)
	}
	if len(responseBody) > maxProviderResponseBytes {
		return Response{}, errors.New("assistant model response exceeds size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Response{}, decodeHTTPError(response.StatusCode, responseBody)
	}

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
		Usage: Usage{
			InputTokens: decoded.Usage.PromptTokens, OutputTokens: decoded.Usage.CompletionTokens, TotalTokens: decoded.Usage.TotalTokens,
		},
	}, nil
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
	Model     string          `json:"model"`
	Messages  []openAIMessage `json:"messages"`
	Tools     []openAITool    `json:"tools,omitempty"`
	MaxTokens int             `json:"max_tokens,omitempty"`
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
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
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
	var payload struct {
		Error struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	code := ""
	switch value := payload.Error.Code.(type) {
	case string:
		code = value
	case float64:
		code = strconv.FormatInt(int64(value), 10)
	}
	return &HTTPError{StatusCode: statusCode, Code: code, Message: payload.Error.Message}
}
