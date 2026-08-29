package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/service/assistant/provider"
	"github.com/01121531/subandnew-api/service/assistant/tool"
)

var (
	ErrInvalidConfiguration = errors.New("invalid assistant runner configuration")
	ErrStepLimit            = errors.New("assistant runner step limit reached")
	ErrToolCallLimit        = errors.New("assistant runner tool call limit reached")
	ErrInvalidModelResponse = errors.New("invalid assistant model response")
	ErrModelRequestTimeout  = errors.New("assistant model request timed out")
)

const maxAuditErrorDetailBytes = 64 << 10

const (
	ErrorStageModelRequest  = "model_request"
	ErrorStageModelStream   = "model_stream"
	ErrorStageModelResponse = "model_response"
	ErrorStageToolExecution = "tool_execution"
	ErrorStageRunner        = "runner"
)

type RunError struct {
	Stage string
	Step  int
	Tool  string
	Cause error
}

func (e *RunError) Error() string {
	if e == nil || e.Cause == nil {
		return "assistant run failed"
	}
	return e.Cause.Error()
}

func (e *RunError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type Config struct {
	SystemPrompt    string
	MaxSteps        int
	MaxToolCalls    int
	MaxOutputTokens int
	RequestTimeout  time.Duration
	RunTimeout      time.Duration
	Progress        func(ProgressEvent)
}

const (
	ProgressModelRequestStarted = "model_request_started"
	ProgressToolStarted         = "tool_started"
	ProgressCompleted           = "completed"
	ProgressFailed              = "failed"
)

type ProgressEvent struct {
	Type string
	Step int
	Tool string
}

type Runner struct {
	client   provider.Client
	registry *tool.Registry
	config   Config
}

type ToolTrace struct {
	CallID               string        `json:"call_id"`
	Name                 string        `json:"name"`
	ArgumentsHash        string        `json:"arguments_hash"`
	ResultHash           string        `json:"result_hash,omitempty"`
	Duration             time.Duration `json:"duration"`
	Error                string        `json:"error,omitempty"`
	ErrorDetail          string        `json:"error_detail,omitempty"`
	ErrorDetailTruncated bool          `json:"error_detail_truncated,omitempty"`
}

type Outcome struct {
	Answer                 string             `json:"answer"`
	Messages               []provider.Message `json:"messages"`
	ToolTraces             []ToolTrace        `json:"tool_traces"`
	Usage                  provider.Usage     `json:"usage"`
	ProviderSteps          int                `json:"provider_steps"`
	ToolCalls              int                `json:"tool_calls"`
	ModelRequests          int                `json:"model_requests"`
	ProviderRetries        int                `json:"provider_retries"`
	RetriedBeforeFirstByte bool               `json:"retried_before_first_byte"`
}

func New(client provider.Client, registry *tool.Registry, config Config) (*Runner, error) {
	if client == nil || registry == nil {
		return nil, fmt.Errorf("%w: provider and tool registry are required", ErrInvalidConfiguration)
	}
	config.SystemPrompt = strings.TrimSpace(config.SystemPrompt)
	if config.SystemPrompt == "" {
		return nil, fmt.Errorf("%w: system prompt is required", ErrInvalidConfiguration)
	}
	if config.MaxSteps == 0 {
		config.MaxSteps = 6
	}
	if config.MaxToolCalls == 0 {
		config.MaxToolCalls = 8
	}
	if config.MaxOutputTokens == 0 {
		config.MaxOutputTokens = 2048
	}
	if config.RequestTimeout == 0 {
		config.RequestTimeout = 120 * time.Second
	}
	if config.RunTimeout == 0 {
		config.RunTimeout = 300 * time.Second
	}
	if config.MaxSteps < 1 || config.MaxSteps > 6 || config.MaxToolCalls < 1 || config.MaxToolCalls > 8 || config.MaxOutputTokens < 1 || config.RequestTimeout < time.Second || config.RequestTimeout > 2*time.Minute || config.RunTimeout < 30*time.Second || config.RunTimeout > 10*time.Minute {
		return nil, ErrInvalidConfiguration
	}
	return &Runner{client: client, registry: registry, config: config}, nil
}

func (r *Runner) Run(ctx context.Context, execution tool.ExecutionContext, conversation []provider.Message) (outcome Outcome, runErr error) {
	defer func() {
		if runErr != nil {
			r.progress(ProgressEvent{Type: ProgressFailed})
		} else if outcome.Answer != "" {
			r.progress(ProgressEvent{Type: ProgressCompleted})
		}
	}()
	if r == nil || r.client == nil || r.registry == nil {
		return Outcome{}, ErrInvalidConfiguration
	}
	if len(conversation) == 0 {
		return Outcome{}, fmt.Errorf("%w: conversation is empty", ErrInvalidConfiguration)
	}
	runContext, cancel := context.WithTimeout(ctx, r.config.RunTimeout)
	defer cancel()

	messages := make([]provider.Message, 0, len(conversation)+r.config.MaxSteps*2+1)
	messages = append(messages, provider.Message{Role: provider.RoleSystem, Content: r.config.SystemPrompt})
	messages = append(messages, conversation...)
	definitions := toolDefinitions(r.registry.List())
	seenCallIDs := make(map[string]struct{})

	for step := 1; step <= r.config.MaxSteps; step++ {
		request := provider.Request{
			Messages: messages, Tools: definitions, MaxOutputTokens: r.config.MaxOutputTokens,
		}
		r.progress(ProgressEvent{Type: ProgressModelRequestStarted, Step: step})
		outcome.ModelRequests++
		requestContext, requestCancel := context.WithTimeout(runContext, r.config.RequestTimeout)
		response, err := generate(requestContext, r.client, request)
		requestCancel()
		if err != nil {
			attempts, retried := provider.RequestAttempts(err)
			outcome.ProviderRetries += max(0, attempts-1)
			outcome.RetriedBeforeFirstByte = outcome.RetriedBeforeFirstByte || retried
			stage := ErrorStageModelRequest
			if strings.Contains(strings.ToLower(err.Error()), "stream") {
				stage = ErrorStageModelStream
			}
			if errors.Is(runContext.Err(), context.DeadlineExceeded) {
				err = context.DeadlineExceeded
			} else if errors.Is(err, context.DeadlineExceeded) {
				err = fmt.Errorf("%w: %v", ErrModelRequestTimeout, err)
			}
			return outcome, &RunError{Stage: stage, Step: step, Cause: err}
		}
		attempts := max(1, response.Attempts)
		outcome.ProviderRetries += max(0, attempts-1)
		outcome.RetriedBeforeFirstByte = outcome.RetriedBeforeFirstByte || response.RetriedBeforeFirstByte
		outcome.ProviderSteps = step
		addUsage(&outcome.Usage, response.Usage)
		if response.Message.Role != provider.RoleAssistant {
			return outcome, &RunError{Stage: ErrorStageModelResponse, Step: step, Cause: fmt.Errorf("%w: expected assistant role", ErrInvalidModelResponse)}
		}
		messages = append(messages, response.Message)
		if len(response.Message.ToolCalls) == 0 {
			answer := strings.TrimSpace(response.Message.Content)
			if answer == "" {
				return outcome, &RunError{Stage: ErrorStageModelResponse, Step: step, Cause: fmt.Errorf("%w: response has no answer or tool call", ErrInvalidModelResponse)}
			}
			outcome.Answer = answer
			outcome.Messages = append([]provider.Message(nil), messages...)
			return outcome, nil
		}
		if outcome.ToolCalls+len(response.Message.ToolCalls) > r.config.MaxToolCalls {
			return outcome, &RunError{Stage: ErrorStageRunner, Step: step, Cause: ErrToolCallLimit}
		}

		for _, call := range response.Message.ToolCalls {
			if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
				return outcome, &RunError{Stage: ErrorStageModelResponse, Step: step, Cause: fmt.Errorf("%w: tool call id and name are required", ErrInvalidModelResponse)}
			}
			if _, exists := seenCallIDs[call.ID]; exists {
				return outcome, &RunError{Stage: ErrorStageModelResponse, Step: step, Cause: fmt.Errorf("%w: duplicate tool call id", ErrInvalidModelResponse)}
			}
			seenCallIDs[call.ID] = struct{}{}
			r.progress(ProgressEvent{Type: ProgressToolStarted, Step: step, Tool: call.Name})
			started := time.Now()
			result, err := r.registry.Execute(runContext, execution, call.Name, call.Arguments)
			trace := ToolTrace{
				CallID: call.ID, Name: call.Name, ArgumentsHash: hashArguments(call.Arguments), Duration: time.Since(started),
			}
			outcome.ToolCalls++
			if err != nil {
				trace.Error = safeToolError(err)
				trace.ErrorDetail, trace.ErrorDetailTruncated = boundedAuditErrorDetail(err.Error())
				outcome.ToolTraces = append(outcome.ToolTraces, trace)
				return outcome, &RunError{Stage: ErrorStageToolExecution, Step: step, Tool: call.Name, Cause: err}
			}
			outcome.ToolTraces = append(outcome.ToolTraces, trace)
			content, err := json.Marshal(result)
			if err != nil {
				return outcome, &RunError{Stage: ErrorStageToolExecution, Step: step, Tool: call.Name, Cause: fmt.Errorf("encode assistant tool result: %w", err)}
			}
			outcome.ToolTraces[len(outcome.ToolTraces)-1].ResultHash = hashBytes(content)
			messages = append(messages, provider.Message{
				Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: string(content),
			})
		}
	}
	return outcome, &RunError{Stage: ErrorStageRunner, Step: r.config.MaxSteps, Cause: ErrStepLimit}
}

func (r *Runner) progress(event ProgressEvent) {
	if r == nil || r.config.Progress == nil {
		return
	}
	r.config.Progress(event)
}

func boundedAuditErrorDetail(value string) (string, bool) {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "�")
	if len(value) <= maxAuditErrorDetailBytes {
		return value, false
	}
	limit := maxAuditErrorDetailBytes
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return value[:limit], true
}

func toolDefinitions(specs []tool.ToolSpec) []provider.ToolDefinition {
	definitions := make([]provider.ToolDefinition, 0, len(specs))
	for _, spec := range specs {
		definitions = append(definitions, provider.ToolDefinition{
			Name: spec.Name, Description: spec.Description, InputSchema: append(json.RawMessage(nil), spec.InputSchema...),
		})
	}
	return definitions
}

func addUsage(total *provider.Usage, current provider.Usage) {
	total.InputTokens += current.InputTokens
	total.OutputTokens += current.OutputTokens
	total.TotalTokens += current.TotalTokens
	total.CachedInputTokens += current.CachedInputTokens
	total.CacheObservedInputTokens += current.CacheObservedInputTokens
}

func generate(ctx context.Context, client provider.Client, request provider.Request) (provider.Response, error) {
	if streaming, ok := client.(provider.StreamingClient); ok {
		return streaming.GenerateStream(ctx, request)
	}
	return client.Generate(ctx, request)
}

func hashArguments(arguments json.RawMessage) string {
	return hashBytes(arguments)
}

func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func safeToolError(err error) string {
	switch {
	case errors.Is(err, tool.ErrToolNotFound):
		return "tool_not_found"
	case errors.Is(err, tool.ErrInvalidArguments):
		return "invalid_arguments"
	case errors.Is(err, tool.ErrAuthorizationDenied):
		return "authorization_denied"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "tool_execution_failed"
	}
}
