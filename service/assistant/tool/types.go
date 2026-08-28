// Package tool provides the read-only tool boundary used by the assistant.
// It intentionally does not depend on a model provider or a domain service.
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Risk describes the sensitivity of a read-only tool. Risk is policy metadata;
// it never replaces authorization performed immediately before execution.
type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// FreshnessState describes how current a tool result is.
type FreshnessState string

const (
	FreshnessLive     FreshnessState = "live"
	FreshnessSnapshot FreshnessState = "snapshot"
	FreshnessStale    FreshnessState = "stale"
	FreshnessUnknown  FreshnessState = "unknown"
)

var (
	ErrInvalidSpec             = errors.New("invalid assistant tool specification")
	ErrDuplicateTool           = errors.New("assistant tool already registered")
	ErrToolNotFound            = errors.New("assistant tool not found")
	ErrInvalidExecutionContext = errors.New("invalid assistant tool execution context")
	ErrInvalidArguments        = errors.New("invalid assistant tool arguments")
	ErrAuthorizationDenied     = errors.New("assistant tool authorization denied")
	ErrInvalidResult           = errors.New("invalid assistant tool result")
)

// Permission is opaque policy metadata understood by the authorization
// callback. It mirrors the control plane's resource/action permission shape
// without coupling this package to a concrete authorization implementation.
type Permission struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

func (permission Permission) validate() error {
	if strings.TrimSpace(permission.Resource) == "" || strings.TrimSpace(permission.Action) == "" {
		return fmt.Errorf("%w: permission resource and action are required", ErrInvalidSpec)
	}
	return nil
}

// ToolSpec is immutable after registration. InputSchema and OutputSchema are
// model-facing JSON schemas; server-side safety is enforced independently by
// strict typed decoding and InputValidator.
type ToolSpec struct {
	Name              string          `json:"name"`
	Version           string          `json:"version"`
	Description       string          `json:"description"`
	Permission        Permission      `json:"permission"`
	Risk              Risk            `json:"risk"`
	ReadOnly          bool            `json:"read_only"`
	Idempotent        bool            `json:"idempotent"`
	InputSchema       json.RawMessage `json:"input_schema"`
	OutputSchema      json.RawMessage `json:"output_schema,omitempty"`
	MaxArgumentsBytes int             `json:"max_arguments_bytes,omitempty"`
}

// ExecutionContext is trusted application state. It must be constructed only
// after a channel identity has been bound to a control-plane user.
type ExecutionContext struct {
	RunID          string `json:"run_id"`
	ConversationID string `json:"conversation_id"`
	RequestID      string `json:"request_id,omitempty"`
	Channel        string `json:"channel"`
	IdentityID     int64  `json:"identity_id"`
	UserID         int    `json:"user_id"`
	UserRole       int    `json:"user_role"`
}

func (execution ExecutionContext) validate() error {
	if strings.TrimSpace(execution.RunID) == "" ||
		strings.TrimSpace(execution.ConversationID) == "" ||
		strings.TrimSpace(execution.Channel) == "" ||
		execution.IdentityID <= 0 ||
		execution.UserID <= 0 {
		return ErrInvalidExecutionContext
	}
	return nil
}

// InputValidator is implemented by typed tool inputs that need semantic
// validation beyond strict JSON decoding (for example ranges or cross-field
// constraints).
type InputValidator interface {
	Validate() error
}

// Provenance identifies an authoritative source used to produce a result.
// Resource should be a stable logical identifier, never a credential or raw
// connection URL.
type Provenance struct {
	Source     string    `json:"source"`
	Resource   string    `json:"resource,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
}

// Freshness makes recency explicit instead of forcing answer renderers to
// guess whether a value is live, cached, stale, or unknown.
type Freshness struct {
	State      FreshnessState `json:"state"`
	ObservedAt time.Time      `json:"observed_at,omitempty"`
	ExpiresAt  time.Time      `json:"expires_at,omitempty"`
	Timezone   string         `json:"timezone,omitempty"`
}

// Output is returned by a strongly typed tool handler.
type Output[T any] struct {
	Data       T            `json:"data"`
	Provenance []Provenance `json:"provenance"`
	Freshness  Freshness    `json:"freshness"`
}

// Result is the type-erased execution result consumed by an orchestrator.
// Data remains JSON so provider-specific or domain-specific values cannot leak
// into the registry API.
type Result struct {
	Data       json.RawMessage `json:"data"`
	Provenance []Provenance    `json:"provenance"`
	Freshness  Freshness       `json:"freshness"`
}

// Handler is the strongly typed implementation of one read-only tool.
type Handler[I any, O any] func(context.Context, ExecutionContext, I) (Output[O], error)

// AuthorizationRequest is evaluated after strict typed argument validation
// but before the handler executes. Arguments contains normalized JSON produced
// from the typed input, not the untrusted wire bytes.
type AuthorizationRequest struct {
	Execution ExecutionContext `json:"execution"`
	Tool      ToolSpec         `json:"tool"`
	Arguments json.RawMessage  `json:"arguments"`
}

// AuthorizeFunc performs the final permission check before each execution.
// Returning any error fails closed and prevents the handler from running.
type AuthorizeFunc func(context.Context, AuthorizationRequest) error
