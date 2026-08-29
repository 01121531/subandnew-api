package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	AssistantRunStatusPending   = "pending"
	AssistantRunStatusRunning   = "running"
	AssistantRunStatusSucceeded = "succeeded"
	AssistantRunStatusFailed    = "failed"
	AssistantRunStatusCancelled = "cancelled"

	AssistantToolCallStatusPending   = "pending"
	AssistantToolCallStatusRunning   = "running"
	AssistantToolCallStatusSucceeded = "succeeded"
	AssistantToolCallStatusFailed    = "failed"
	AssistantToolCallStatusDenied    = "denied"

	AssistantToolRiskLow      = "low"
	AssistantToolRiskMedium   = "medium"
	AssistantToolRiskHigh     = "high"
	AssistantToolRiskCritical = "critical"
)

// AssistantRun persists the lifecycle and bounded accounting metadata for one
// assistant turn. It intentionally stores no full prompt or model response.
type AssistantRun struct {
	ID                       int64  `json:"id" gorm:"primaryKey"`
	RunID                    string `json:"run_id" gorm:"type:varchar(64);not null;uniqueIndex:uidx_assistant_run_public_id"`
	ConversationID           int64  `json:"conversation_id" gorm:"not null;index"`
	TriggerMessageID         int64  `json:"trigger_message_id" gorm:"not null;index"`
	ModelProfileID           int64  `json:"model_profile_id" gorm:"not null;default:0;index"`
	Model                    string `json:"model" gorm:"type:varchar(160);not null"`
	PromptVersion            string `json:"prompt_version" gorm:"type:varchar(64);not null"`
	Status                   string `json:"status" gorm:"type:varchar(32);not null;default:'pending';index"`
	DeadlineAt               int64  `json:"deadline_at" gorm:"bigint;not null;default:0;index"`
	RequestTimeoutSeconds    int    `json:"request_timeout_seconds" gorm:"not null;default:0"`
	ModelRequestCount        int    `json:"model_request_count" gorm:"not null;default:0"`
	ProviderRetryCount       int    `json:"provider_retry_count" gorm:"not null;default:0"`
	RetriedBeforeFirstByte   bool   `json:"retried_before_first_byte" gorm:"not null;default:false"`
	InputTokens              int64  `json:"input_tokens" gorm:"bigint;not null;default:0"`
	OutputTokens             int64  `json:"output_tokens" gorm:"bigint;not null;default:0"`
	TotalTokens              int64  `json:"total_tokens" gorm:"bigint;not null;default:0"`
	CachedInputTokens        int64  `json:"cached_input_tokens" gorm:"bigint;not null;default:0"`
	CacheObservedInputTokens int64  `json:"cache_observed_input_tokens" gorm:"bigint;not null;default:0"`
	Cost                     string `json:"cost" gorm:"type:varchar(64);not null;default:'0'"`
	ErrorCode                string `json:"error_code,omitempty" gorm:"type:varchar(128);index"`
	ErrorStage               string `json:"error_stage,omitempty" gorm:"type:varchar(64);index"`
	ErrorReasonCode          string `json:"error_reason_code,omitempty" gorm:"type:varchar(128);index"`
	ErrorDetail              string `json:"error_detail,omitempty" gorm:"type:text"`
	ErrorDetailTruncated     bool   `json:"error_detail_truncated,omitempty" gorm:"not null;default:false"`
	ProviderStatusCode       int    `json:"provider_status_code,omitempty" gorm:"not null;default:0"`
	ProviderErrorCode        string `json:"provider_error_code,omitempty" gorm:"type:varchar(128)"`
	TraceID                  string `json:"trace_id" gorm:"type:varchar(64);not null;index"`
	StartedAt                int64  `json:"started_at" gorm:"bigint;not null;default:0"`
	FinishedAt               int64  `json:"finished_at" gorm:"bigint;not null;default:0"`
	CreatedAt                int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt                int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantRun) TableName() string { return "assistant_runs" }

func (run *AssistantRun) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if run.Status == "" {
		run.Status = AssistantRunStatusPending
	}
	if run.Cost == "" {
		run.Cost = "0"
	}
	if run.CreatedAt == 0 {
		run.CreatedAt = now
	}
	run.UpdatedAt = now
	return nil
}

func (run *AssistantRun) BeforeUpdate(_ *gorm.DB) error {
	run.UpdatedAt = common.GetTimestamp()
	return nil
}

// AssistantToolCall keeps only redacted arguments and a result digest. Full
// tool results are deliberately outside this long-lived audit model.
type AssistantToolCall struct {
	ID                   int64  `json:"id" gorm:"primaryKey"`
	RunID                int64  `json:"run_id" gorm:"not null;uniqueIndex:uidx_assistant_tool_call_order,priority:1;index"`
	Sequence             int    `json:"sequence" gorm:"not null;uniqueIndex:uidx_assistant_tool_call_order,priority:2"`
	Tool                 string `json:"tool" gorm:"type:varchar(128);not null;index"`
	ArgumentsRedacted    string `json:"arguments_redacted,omitempty" gorm:"type:text;not null"`
	ResultDigest         string `json:"result_digest,omitempty" gorm:"type:char(64)"`
	Status               string `json:"status" gorm:"type:varchar(32);not null;default:'pending';index"`
	Permission           string `json:"permission" gorm:"type:varchar(128);not null;index"`
	Risk                 string `json:"risk" gorm:"type:varchar(16);not null;default:'low';index"`
	LatencyMs            int64  `json:"latency_ms" gorm:"bigint;not null;default:0"`
	ErrorCode            string `json:"error_code,omitempty" gorm:"type:varchar(128)"`
	ErrorDetail          string `json:"error_detail,omitempty" gorm:"type:text"`
	ErrorDetailTruncated bool   `json:"error_detail_truncated,omitempty" gorm:"not null;default:false"`
	StartedAt            int64  `json:"started_at" gorm:"bigint;not null;default:0"`
	FinishedAt           int64  `json:"finished_at" gorm:"bigint;not null;default:0"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantToolCall) TableName() string { return "assistant_tool_calls" }

func (call *AssistantToolCall) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if call.Status == "" {
		call.Status = AssistantToolCallStatusPending
	}
	if call.Risk == "" {
		call.Risk = AssistantToolRiskLow
	}
	if call.CreatedAt == 0 {
		call.CreatedAt = now
	}
	call.UpdatedAt = now
	return nil
}

func (call *AssistantToolCall) BeforeUpdate(_ *gorm.DB) error {
	call.UpdatedAt = common.GetTimestamp()
	return nil
}
