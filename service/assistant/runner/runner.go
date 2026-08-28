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

	"github.com/01121531/subandnew-api/service/assistant/provider"
	"github.com/01121531/subandnew-api/service/assistant/tool"
)

var (
	ErrInvalidConfiguration = errors.New("invalid assistant runner configuration")
	ErrStepLimit            = errors.New("assistant runner step limit reached")
	ErrToolCallLimit        = errors.New("assistant runner tool call limit reached")
	ErrInvalidModelResponse = errors.New("invalid assistant model response")
)

type Config struct {
	SystemPrompt    string
	MaxSteps        int
	MaxToolCalls    int
	MaxOutputTokens int
	Timeout         time.Duration
}

type Runner struct {
	client   provider.Client
	registry *tool.Registry
	config   Config
}

type ToolTrace struct {
	CallID        string        `json:"call_id"`
	Name          string        `json:"name"`
	ArgumentsHash string        `json:"arguments_hash"`
	ResultHash    string        `json:"result_hash,omitempty"`
	Duration      time.Duration `json:"duration"`
	Error         string        `json:"error,omitempty"`
}

type Outcome struct {
	Answer        string             `json:"answer"`
	Messages      []provider.Message `json:"messages"`
	ToolTraces    []ToolTrace        `json:"tool_traces"`
	Usage         provider.Usage     `json:"usage"`
	ProviderSteps int                `json:"provider_steps"`
	ToolCalls     int                `json:"tool_calls"`
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
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.MaxSteps < 1 || config.MaxSteps > 6 || config.MaxToolCalls < 1 || config.MaxToolCalls > 8 || config.MaxOutputTokens < 1 || config.Timeout < time.Second || config.Timeout > time.Minute {
		return nil, ErrInvalidConfiguration
	}
	return &Runner{client: client, registry: registry, config: config}, nil
}

func (r *Runner) Run(ctx context.Context, execution tool.ExecutionContext, conversation []provider.Message) (Outcome, error) {
	if r == nil || r.client == nil || r.registry == nil {
		return Outcome{}, ErrInvalidConfiguration
	}
	if len(conversation) == 0 {
		return Outcome{}, fmt.Errorf("%w: conversation is empty", ErrInvalidConfiguration)
	}
	runContext, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()

	messages := make([]provider.Message, 0, len(conversation)+r.config.MaxSteps*2+1)
	messages = append(messages, provider.Message{Role: provider.RoleSystem, Content: r.config.SystemPrompt})
	messages = append(messages, conversation...)
	definitions := toolDefinitions(r.registry.List())
	outcome := Outcome{}
	seenCallIDs := make(map[string]struct{})

	for step := 1; step <= r.config.MaxSteps; step++ {
		response, err := r.client.Generate(runContext, provider.Request{
			Messages: messages, Tools: definitions, MaxOutputTokens: r.config.MaxOutputTokens,
		})
		if err != nil {
			return outcome, err
		}
		outcome.ProviderSteps = step
		addUsage(&outcome.Usage, response.Usage)
		if response.Message.Role != provider.RoleAssistant {
			return outcome, fmt.Errorf("%w: expected assistant role", ErrInvalidModelResponse)
		}
		messages = append(messages, response.Message)
		if len(response.Message.ToolCalls) == 0 {
			answer := strings.TrimSpace(response.Message.Content)
			if answer == "" {
				return outcome, fmt.Errorf("%w: response has no answer or tool call", ErrInvalidModelResponse)
			}
			outcome.Answer = answer
			outcome.Messages = append([]provider.Message(nil), messages...)
			return outcome, nil
		}
		if outcome.ToolCalls+len(response.Message.ToolCalls) > r.config.MaxToolCalls {
			return outcome, ErrToolCallLimit
		}

		for _, call := range response.Message.ToolCalls {
			if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
				return outcome, fmt.Errorf("%w: tool call id and name are required", ErrInvalidModelResponse)
			}
			if _, exists := seenCallIDs[call.ID]; exists {
				return outcome, fmt.Errorf("%w: duplicate tool call id", ErrInvalidModelResponse)
			}
			seenCallIDs[call.ID] = struct{}{}
			started := time.Now()
			result, err := r.registry.Execute(runContext, execution, call.Name, call.Arguments)
			trace := ToolTrace{
				CallID: call.ID, Name: call.Name, ArgumentsHash: hashArguments(call.Arguments), Duration: time.Since(started),
			}
			outcome.ToolCalls++
			if err != nil {
				trace.Error = safeToolError(err)
				outcome.ToolTraces = append(outcome.ToolTraces, trace)
				return outcome, err
			}
			outcome.ToolTraces = append(outcome.ToolTraces, trace)
			content, err := json.Marshal(result)
			if err != nil {
				return outcome, fmt.Errorf("encode assistant tool result: %w", err)
			}
			outcome.ToolTraces[len(outcome.ToolTraces)-1].ResultHash = hashBytes(content)
			messages = append(messages, provider.Message{
				Role: provider.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: string(content),
			})
		}
	}
	return outcome, ErrStepLimit
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
