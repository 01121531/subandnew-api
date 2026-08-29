package provider

import (
	"context"
	"encoding/json"
	"errors"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type Request struct {
	Messages        []Message
	Tools           []ToolDefinition
	MaxOutputTokens int
}

type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	TotalTokens              int `json:"total_tokens"`
	CachedInputTokens        int `json:"cached_input_tokens"`
	CacheObservedInputTokens int `json:"cache_observed_input_tokens"`
}

type Response struct {
	Message                Message `json:"message"`
	FinishReason           string  `json:"finish_reason"`
	Usage                  Usage   `json:"usage"`
	Attempts               int     `json:"attempts"`
	RetriedBeforeFirstByte bool    `json:"retried_before_first_byte"`
}

type RequestError struct {
	Cause                  error
	Attempts               int
	RetriedBeforeFirstByte bool
}

func (e *RequestError) Error() string {
	if e == nil || e.Cause == nil {
		return "assistant model request failed"
	}
	return e.Cause.Error()
}

func (e *RequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func RequestAttempts(err error) (int, bool) {
	var requestError *RequestError
	if !errors.As(err, &requestError) {
		return 1, false
	}
	return max(1, requestError.Attempts), requestError.RetriedBeforeFirstByte
}

type Client interface {
	Generate(context.Context, Request) (Response, error)
}

// StreamingClient is optional so existing providers and test doubles can keep
// using the non-streaming Client contract. Runners prefer it when available.
type StreamingClient interface {
	GenerateStream(context.Context, Request) (Response, error)
}

var ErrInvalidRequest = errors.New("invalid assistant model request")
