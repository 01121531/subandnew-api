package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeInput struct {
	InstanceID int    `json:"instance_id"`
	Window     string `json:"window"`
}

func (input fakeInput) Validate() error {
	if input.InstanceID <= 0 {
		return errors.New("instance_id must be positive")
	}
	if input.Window != "day" && input.Window != "week" {
		return errors.New("window is invalid")
	}
	return nil
}

type fakeData struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

func validSpec(name string) ToolSpec {
	return ToolSpec{
		Name:        name,
		Version:     "v1",
		Description: "Read a fake status.",
		Permission:  Permission{Resource: "managed_instance", Action: "view"},
		Risk:        RiskLow,
		ReadOnly:    true,
		Idempotent:  true,
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"instance_id":{"type":"integer"},
				"window":{"type":"string","enum":["day","week"]}
			},
			"required":["instance_id","window"],
			"additionalProperties":false
		}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func validExecution() ExecutionContext {
	return ExecutionContext{
		RunID:          "run-1",
		ConversationID: "conversation-1",
		RequestID:      "request-1",
		Channel:        "openbot",
		IdentityID:     1,
		UserID:         42,
		UserRole:       10,
	}
}

func validOutput(now time.Time) Output[fakeData] {
	return Output[fakeData]{
		Data: fakeData{Status: "healthy", Count: 3},
		Provenance: []Provenance{{
			Source:     "managed_instance_snapshot",
			Resource:   "instance:7",
			ObservedAt: now,
		}},
		Freshness: Freshness{
			State:      FreshnessSnapshot,
			ObservedAt: now,
			ExpiresAt:  now.Add(time.Minute),
		},
	}
}

func newAllowRegistry(t *testing.T) *Registry {
	t.Helper()
	registry, err := NewRegistry(func(context.Context, AuthorizationRequest) error { return nil })
	require.NoError(t, err)
	return registry
}

func TestNewRegistryRequiresAuthorization(t *testing.T) {
	registry, err := NewRegistry(nil)
	require.Nil(t, registry)
	require.ErrorIs(t, err, ErrInvalidSpec)
}

func TestRegisterRejectsInvalidSpecification(t *testing.T) {
	handler := func(context.Context, ExecutionContext, fakeInput) (Output[fakeData], error) {
		return validOutput(time.Now()), nil
	}
	tests := []struct {
		name   string
		mutate func(*ToolSpec)
	}{
		{name: "invalid name", mutate: func(spec *ToolSpec) { spec.Name = "Bad Name" }},
		{name: "missing version", mutate: func(spec *ToolSpec) { spec.Version = "" }},
		{name: "missing description", mutate: func(spec *ToolSpec) { spec.Description = "" }},
		{name: "missing permission resource", mutate: func(spec *ToolSpec) { spec.Permission.Resource = "" }},
		{name: "missing permission action", mutate: func(spec *ToolSpec) { spec.Permission.Action = "" }},
		{name: "invalid risk", mutate: func(spec *ToolSpec) { spec.Risk = "critical" }},
		{name: "not read only", mutate: func(spec *ToolSpec) { spec.ReadOnly = false }},
		{name: "not idempotent", mutate: func(spec *ToolSpec) { spec.Idempotent = false }},
		{name: "invalid input schema", mutate: func(spec *ToolSpec) { spec.InputSchema = json.RawMessage(`{`) }},
		{name: "non object input schema", mutate: func(spec *ToolSpec) { spec.InputSchema = json.RawMessage(`{"type":"array"}`) }},
		{name: "invalid output schema", mutate: func(spec *ToolSpec) { spec.OutputSchema = json.RawMessage(`{`) }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := newAllowRegistry(t)
			spec := validSpec("get_status")
			test.mutate(&spec)
			err := Register(registry, spec, Handler[fakeInput, fakeData](handler))
			require.ErrorIs(t, err, ErrInvalidSpec)
			require.Empty(t, registry.List())
		})
	}
}

func TestRegisterRejectsNilInputsAndDuplicates(t *testing.T) {
	spec := validSpec("get_status")
	handler := Handler[fakeInput, fakeData](func(context.Context, ExecutionContext, fakeInput) (Output[fakeData], error) {
		return validOutput(time.Now()), nil
	})

	require.ErrorIs(t, Register[fakeInput, fakeData](nil, spec, handler), ErrInvalidSpec)
	registry := newAllowRegistry(t)
	require.ErrorIs(t, Register[fakeInput, fakeData](registry, spec, nil), ErrInvalidSpec)
	require.NoError(t, Register(registry, spec, handler))
	require.ErrorIs(t, Register(registry, spec, handler), ErrDuplicateTool)
}

func TestExecuteValidatesAuthorizesThenRunsTypedHandler(t *testing.T) {
	now := time.Date(2026, time.August, 28, 12, 0, 0, 0, time.UTC)
	var events []string
	registry, err := NewRegistry(func(_ context.Context, request AuthorizationRequest) error {
		events = append(events, "authorize")
		require.Equal(t, validExecution(), request.Execution)
		require.Equal(t, "get_status", request.Tool.Name)
		require.JSONEq(t, `{"instance_id":7,"window":"day"}`, string(request.Arguments))
		return nil
	})
	require.NoError(t, err)
	err = Register(registry, validSpec("get_status"), func(_ context.Context, execution ExecutionContext, input fakeInput) (Output[fakeData], error) {
		events = append(events, "handler")
		require.Equal(t, validExecution(), execution)
		require.Equal(t, fakeInput{InstanceID: 7, Window: "day"}, input)
		return validOutput(now), nil
	})
	require.NoError(t, err)

	result, err := registry.Execute(context.Background(), validExecution(), "get_status", json.RawMessage(`{
		"window": "day",
		"instance_id": 7
	}`))
	require.NoError(t, err)
	require.Equal(t, []string{"authorize", "handler"}, events)
	require.JSONEq(t, `{"status":"healthy","count":3}`, string(result.Data))
	require.Equal(t, validOutput(now).Provenance, result.Provenance)
	require.Equal(t, validOutput(now).Freshness, result.Freshness)
}

func TestExecuteRejectsArgumentsBeforeAuthorizationAndHandler(t *testing.T) {
	var authorizationCalls atomic.Int32
	var handlerCalls atomic.Int32
	registry, err := NewRegistry(func(context.Context, AuthorizationRequest) error {
		authorizationCalls.Add(1)
		return nil
	})
	require.NoError(t, err)
	spec := validSpec("get_status")
	spec.MaxArgumentsBytes = 64
	require.NoError(t, Register(registry, spec, func(context.Context, ExecutionContext, fakeInput) (Output[fakeData], error) {
		handlerCalls.Add(1)
		return validOutput(time.Now()), nil
	}))

	tests := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "empty", raw: nil},
		{name: "top level array", raw: json.RawMessage(`[]`)},
		{name: "top level null", raw: json.RawMessage(`null`)},
		{name: "unknown field", raw: json.RawMessage(`{"instance_id":7,"window":"day","secret":"x"}`)},
		{name: "wrong type", raw: json.RawMessage(`{"instance_id":"7","window":"day"}`)},
		{name: "semantic validation", raw: json.RawMessage(`{"instance_id":0,"window":"month"}`)},
		{name: "trailing value", raw: json.RawMessage(`{"instance_id":7,"window":"day"} {}`)},
		{name: "too large", raw: json.RawMessage(`{"instance_id":7,"window":"day","padding":"abcdefghijklmnopqrstuvwxyz"}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := registry.Execute(context.Background(), validExecution(), "get_status", test.raw)
			require.ErrorIs(t, err, ErrInvalidArguments)
		})
	}
	require.Zero(t, authorizationCalls.Load())
	require.Zero(t, handlerCalls.Load())
}

func TestExecuteFailsClosedWhenAuthorizationFails(t *testing.T) {
	denied := errors.New("managed_instance.view denied")
	var handlerCalls atomic.Int32
	registry, err := NewRegistry(func(context.Context, AuthorizationRequest) error { return denied })
	require.NoError(t, err)
	require.NoError(t, Register(registry, validSpec("get_status"), func(context.Context, ExecutionContext, fakeInput) (Output[fakeData], error) {
		handlerCalls.Add(1)
		return validOutput(time.Now()), nil
	}))

	_, err = registry.Execute(context.Background(), validExecution(), "get_status", json.RawMessage(`{"instance_id":7,"window":"day"}`))
	require.ErrorIs(t, err, ErrAuthorizationDenied)
	require.Contains(t, err.Error(), denied.Error())
	require.Zero(t, handlerCalls.Load())
}

func TestExecutePreservesHandlerError(t *testing.T) {
	handlerErr := errors.New("fake backend unavailable")
	registry := newAllowRegistry(t)
	require.NoError(t, Register(registry, validSpec("get_status"), func(context.Context, ExecutionContext, fakeInput) (Output[fakeData], error) {
		return Output[fakeData]{}, handlerErr
	}))

	_, err := registry.Execute(context.Background(), validExecution(), "get_status", json.RawMessage(`{"instance_id":7,"window":"day"}`))
	require.ErrorIs(t, err, handlerErr)
}

func TestExecuteRejectsInvalidContextUnknownToolAndCancellation(t *testing.T) {
	var authorizationCalls atomic.Int32
	registry, err := NewRegistry(func(context.Context, AuthorizationRequest) error {
		authorizationCalls.Add(1)
		return nil
	})
	require.NoError(t, err)

	invalid := validExecution()
	invalid.UserID = 0
	_, err = registry.Execute(context.Background(), invalid, "missing", json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrInvalidExecutionContext)

	_, err = registry.Execute(context.Background(), validExecution(), "missing", json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrToolNotFound)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = registry.Execute(ctx, validExecution(), "missing", json.RawMessage(`{}`))
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, authorizationCalls.Load())
}

func TestExecuteRequiresValidProvenanceAndFreshness(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		output Output[fakeData]
	}{
		{name: "missing provenance", output: Output[fakeData]{Freshness: Freshness{State: FreshnessUnknown}}},
		{name: "blank source", output: Output[fakeData]{Provenance: []Provenance{{Source: " "}}, Freshness: Freshness{State: FreshnessUnknown}}},
		{name: "invalid freshness", output: Output[fakeData]{Provenance: []Provenance{{Source: "fake"}}, Freshness: Freshness{State: "cached"}}},
		{name: "missing observed time", output: Output[fakeData]{Provenance: []Provenance{{Source: "fake"}}, Freshness: Freshness{State: FreshnessLive}}},
		{name: "expiry without observation", output: Output[fakeData]{Provenance: []Provenance{{Source: "fake"}}, Freshness: Freshness{State: FreshnessUnknown, ExpiresAt: now}}},
		{name: "expiry before observation", output: Output[fakeData]{Provenance: []Provenance{{Source: "fake"}}, Freshness: Freshness{State: FreshnessSnapshot, ObservedAt: now, ExpiresAt: now.Add(-time.Second)}}},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := newAllowRegistry(t)
			name := fmt.Sprintf("get_status_%d", index)
			spec := validSpec(name)
			require.NoError(t, Register(registry, spec, func(context.Context, ExecutionContext, fakeInput) (Output[fakeData], error) {
				return test.output, nil
			}))
			_, err := registry.Execute(context.Background(), validExecution(), name, json.RawMessage(`{"instance_id":7,"window":"day"}`))
			require.ErrorIs(t, err, ErrInvalidResult)
		})
	}
}

func TestListIsSortedAndReturnsSchemaCopies(t *testing.T) {
	registry := newAllowRegistry(t)
	handler := Handler[fakeInput, fakeData](func(context.Context, ExecutionContext, fakeInput) (Output[fakeData], error) {
		return validOutput(time.Now()), nil
	})
	require.NoError(t, Register(registry, validSpec("zeta_status"), handler))
	require.NoError(t, Register(registry, validSpec("alpha_status"), handler))

	first := registry.List()
	require.Len(t, first, 2)
	require.Equal(t, "alpha_status", first[0].Name)
	require.Equal(t, "zeta_status", first[1].Name)
	first[0].InputSchema[0] = 'x'
	first[0].OutputSchema[0] = 'x'

	second := registry.List()
	require.True(t, json.Valid(second[0].InputSchema))
	require.True(t, json.Valid(second[0].OutputSchema))
}

func TestRegistrySupportsConcurrentReadsAndExecutions(t *testing.T) {
	registry := newAllowRegistry(t)
	require.NoError(t, Register(registry, validSpec("get_status"), func(context.Context, ExecutionContext, fakeInput) (Output[fakeData], error) {
		return validOutput(time.Now()), nil
	}))

	const workers = 32
	var wait sync.WaitGroup
	wait.Add(workers)
	errorsFound := make(chan error, workers)
	for range workers {
		go func() {
			defer wait.Done()
			if len(registry.List()) != 1 {
				errorsFound <- errors.New("unexpected registry size")
				return
			}
			_, err := registry.Execute(context.Background(), validExecution(), "get_status", json.RawMessage(`{"instance_id":7,"window":"day"}`))
			if err != nil {
				errorsFound <- err
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		require.NoError(t, err)
	}
}
