package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	AssistantOutboxStatusPending    = "pending"
	AssistantOutboxStatusSending    = "sending"
	AssistantOutboxStatusSucceeded  = "succeeded"
	AssistantOutboxStatusFailed     = "failed"
	AssistantOutboxStatusUnknown    = "unknown"
	AssistantOutboxStatusDeadLetter = "dead_letter"
)

// AssistantOutbox is the durable source of truth for outgoing replies. A
// globally unique ReplyKey makes retries idempotent across worker restarts.
type AssistantOutbox struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	ChannelID         int64  `json:"channel_id" gorm:"not null;index"`
	ConversationID    int64  `json:"conversation_id" gorm:"not null;index"`
	RunID             int64  `json:"run_id" gorm:"not null;default:0;index"`
	ReplyKey          string `json:"reply_key" gorm:"type:varchar(191);not null;uniqueIndex:uidx_assistant_outbox_reply_key"`
	Payload           string `json:"-" gorm:"type:text;not null"`
	PayloadKeyVersion string `json:"-" gorm:"type:varchar(50);not null"`
	Status            string `json:"status" gorm:"type:varchar(32);not null;default:'pending';index:idx_assistant_outbox_due,priority:1"`
	Attempt           int    `json:"attempt" gorm:"not null;default:0"`
	NextAttemptAt     int64  `json:"next_attempt_at" gorm:"bigint;not null;default:0;index:idx_assistant_outbox_due,priority:2"`
	ClaimOwner        string `json:"-" gorm:"type:varchar(191);not null;default:'';index"`
	LeaseUntil        int64  `json:"-" gorm:"bigint;not null;default:0;index"`
	DeliveryStartedAt int64  `json:"delivery_started_at" gorm:"bigint;not null;default:0;index"`
	RemoteResult      string `json:"-" gorm:"type:text"`
	ErrorCode         string `json:"error_code,omitempty" gorm:"type:varchar(128)"`
	SentAt            int64  `json:"sent_at" gorm:"bigint;not null;default:0"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantOutbox) TableName() string { return "assistant_outbox" }

func (outbox *AssistantOutbox) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if outbox.Status == "" {
		outbox.Status = AssistantOutboxStatusPending
	}
	if outbox.CreatedAt == 0 {
		outbox.CreatedAt = now
	}
	outbox.UpdatedAt = now
	return nil
}

func (outbox *AssistantOutbox) BeforeUpdate(_ *gorm.DB) error {
	outbox.UpdatedAt = common.GetTimestamp()
	return nil
}
