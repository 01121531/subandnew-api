package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
)

const defaultMaxArgumentsBytes = 64 * 1024

var toolNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type registeredTool struct {
	spec    ToolSpec
	execute func(context.Context, ExecutionContext, json.RawMessage) (Result, error)
}

// Registry stores immutable read-only tools and executes them behind a
// mandatory authorization callback. It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	authorize AuthorizeFunc
	tools     map[string]registeredTool
}

// NewRegistry constructs a fail-closed registry. Authorization is mandatory
// because all tool calls originate from model-controlled output.
func NewRegistry(authorize AuthorizeFunc) (*Registry, error) {
	if authorize == nil {
		return nil, fmt.Errorf("%w: authorization callback is required", ErrInvalidSpec)
	}
	return &Registry{authorize: authorize, tools: make(map[string]registeredTool)}, nil
}

// Register adds a strongly typed read-only tool to registry.
func Register[I any, O any](registry *Registry, spec ToolSpec, handler Handler[I, O]) error {
	if registry == nil {
		return fmt.Errorf("%w: registry is required", ErrInvalidSpec)
	}
	if handler == nil {
		return fmt.Errorf("%w: handler is required", ErrInvalidSpec)
	}
	normalized, err := normalizeSpec(spec)
	if err != nil {
		return err
	}

	entry := registeredTool{
		spec: normalized,
		execute: func(ctx context.Context, execution ExecutionContext, raw json.RawMessage) (Result, error) {
			input, canonical, err := decodeArguments[I](raw, normalized.MaxArgumentsBytes)
			if err != nil {
				return Result{}, err
			}
			if err := registry.authorize(ctx, AuthorizationRequest{
				Execution: execution,
				Tool:      cloneSpec(normalized),
				Arguments: canonical,
			}); err != nil {
				return Result{}, fmt.Errorf("%w: %v", ErrAuthorizationDenied, err)
			}
			output, err := handler(ctx, execution, input)
			if err != nil {
				return Result{}, err
			}
			return eraseOutput(output)
		},
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.tools[normalized.Name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateTool, normalized.Name)
	}
	registry.tools[normalized.Name] = entry
	return nil
}

// Execute validates trusted execution state, looks up a tool without holding a
// registry lock during user code, then performs validation, authorization, and
// execution in that order.
func (registry *Registry) Execute(ctx context.Context, execution ExecutionContext, name string, arguments json.RawMessage) (Result, error) {
	if registry == nil {
		return Result{}, ErrToolNotFound
	}
	if err := execution.validate(); err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	registry.mu.RLock()
	entry, exists := registry.tools[name]
	registry.mu.RUnlock()
	if !exists {
		return Result{}, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	return entry.execute(ctx, execution, arguments)
}

// List returns registered specifications in stable name order. Returned schema
// bytes are cloned so callers cannot mutate registry state.
func (registry *Registry) List() []ToolSpec {
	if registry == nil {
		return nil
	}
	registry.mu.RLock()
	result := make([]ToolSpec, 0, len(registry.tools))
	for _, entry := range registry.tools {
		result = append(result, cloneSpec(entry.spec))
	}
	registry.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func normalizeSpec(spec ToolSpec) (ToolSpec, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Version = strings.TrimSpace(spec.Version)
	spec.Description = strings.TrimSpace(spec.Description)
	if !toolNamePattern.MatchString(spec.Name) {
		return ToolSpec{}, fmt.Errorf("%w: invalid name", ErrInvalidSpec)
	}
	if spec.Version == "" || spec.Description == "" {
		return ToolSpec{}, fmt.Errorf("%w: version and description are required", ErrInvalidSpec)
	}
	if err := spec.Permission.validate(); err != nil {
		return ToolSpec{}, err
	}
	if !validRisk(spec.Risk) {
		return ToolSpec{}, fmt.Errorf("%w: invalid risk", ErrInvalidSpec)
	}
	if !spec.ReadOnly || !spec.Idempotent {
		return ToolSpec{}, fmt.Errorf("%w: registry accepts only read-only idempotent tools", ErrInvalidSpec)
	}
	if err := validateObjectSchema(spec.InputSchema); err != nil {
		return ToolSpec{}, fmt.Errorf("%w: input schema: %v", ErrInvalidSpec, err)
	}
	if len(spec.OutputSchema) > 0 && !json.Valid(spec.OutputSchema) {
		return ToolSpec{}, fmt.Errorf("%w: output schema is not valid JSON", ErrInvalidSpec)
	}
	if spec.MaxArgumentsBytes <= 0 {
		spec.MaxArgumentsBytes = defaultMaxArgumentsBytes
	}
	spec.InputSchema = bytes.Clone(spec.InputSchema)
	spec.OutputSchema = bytes.Clone(spec.OutputSchema)
	return spec, nil
}

func validateObjectSchema(raw json.RawMessage) error {
	if !json.Valid(raw) {
		return errors.New("not valid JSON")
	}
	var schema struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	if schema.Type != "object" {
		return errors.New("top-level type must be object")
	}
	return nil
}

func decodeArguments[I any](raw json.RawMessage, limit int) (I, json.RawMessage, error) {
	var zero I
	if len(raw) == 0 || len(raw) > limit {
		return zero, nil, ErrInvalidArguments
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return zero, nil, fmt.Errorf("%w: top-level value must be an object", ErrInvalidArguments)
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var input I
	if err := decoder.Decode(&input); err != nil {
		return zero, nil, fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return zero, nil, fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	if validator, ok := any(input).(InputValidator); ok {
		if err := validator.Validate(); err != nil {
			return zero, nil, fmt.Errorf("%w: %v", ErrInvalidArguments, err)
		}
	}
	canonical, err := json.Marshal(input)
	if err != nil {
		return zero, nil, fmt.Errorf("%w: arguments cannot be normalized", ErrInvalidArguments)
	}
	return input, canonical, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values are not allowed")
	}
	return err
}

func eraseOutput[O any](output Output[O]) (Result, error) {
	if err := validateResultMetadata(output.Provenance, output.Freshness); err != nil {
		return Result{}, err
	}
	data, err := json.Marshal(output.Data)
	if err != nil {
		return Result{}, fmt.Errorf("%w: data cannot be encoded", ErrInvalidResult)
	}
	return Result{
		Data:       data,
		Provenance: append([]Provenance(nil), output.Provenance...),
		Freshness:  output.Freshness,
	}, nil
}

func validateResultMetadata(provenance []Provenance, freshness Freshness) error {
	if len(provenance) == 0 {
		return fmt.Errorf("%w: provenance is required", ErrInvalidResult)
	}
	for _, source := range provenance {
		if strings.TrimSpace(source.Source) == "" {
			return fmt.Errorf("%w: provenance source is required", ErrInvalidResult)
		}
	}
	if !validFreshnessState(freshness.State) {
		return fmt.Errorf("%w: invalid freshness state", ErrInvalidResult)
	}
	if freshness.State != FreshnessUnknown && freshness.ObservedAt.IsZero() {
		return fmt.Errorf("%w: observed time is required", ErrInvalidResult)
	}
	if !freshness.ExpiresAt.IsZero() && freshness.ObservedAt.IsZero() {
		return fmt.Errorf("%w: expiry requires observed time", ErrInvalidResult)
	}
	if !freshness.ExpiresAt.IsZero() && freshness.ExpiresAt.Before(freshness.ObservedAt) {
		return fmt.Errorf("%w: expiry precedes observation", ErrInvalidResult)
	}
	return nil
}

func validRisk(risk Risk) bool {
	return risk == RiskLow || risk == RiskMedium || risk == RiskHigh
}

func validFreshnessState(state FreshnessState) bool {
	return state == FreshnessLive || state == FreshnessSnapshot || state == FreshnessStale || state == FreshnessUnknown
}

func cloneSpec(spec ToolSpec) ToolSpec {
	spec.InputSchema = bytes.Clone(spec.InputSchema)
	spec.OutputSchema = bytes.Clone(spec.OutputSchema)
	return spec
}
