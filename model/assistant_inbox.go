package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	AssistantInboundStatusPending    = "pending"
	AssistantInboundStatusProcessing = "processing"
	AssistantInboundStatusSucceeded  = "succeeded"
	AssistantInboundStatusFailed     = "failed"
	AssistantInboundStatusDeadLetter = "dead_letter"
)

// AssistantInboundEvent is the durable inbox record and the source of truth
// for external messages. Payload and cursor are internal protocol data.
type AssistantInboundEvent struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	ChannelID         int64  `json:"channel_id" gorm:"not null;uniqueIndex:uidx_assistant_inbound_external,priority:1;index"`
	AccountID         string `json:"account_id" gorm:"type:varchar(191);not null;index"`
	ExternalMessageID string `json:"external_message_id" gorm:"type:varchar(191);not null;uniqueIndex:uidx_assistant_inbound_external,priority:2"`
	Seq               int64  `json:"seq" gorm:"bigint;not null;default:0;index"`
	PeerID            string `json:"peer_id" gorm:"type:varchar(191);not null;index"`
	ExternalUserID    string `json:"external_user_id" gorm:"type:varchar(191);not null;index"`
	Payload           string `json:"-" gorm:"type:text;not null"`
	PayloadKeyVersion string `json:"-" gorm:"type:varchar(50);not null"`
	Status            string `json:"status" gorm:"type:varchar(32);not null;default:'pending';index:idx_assistant_inbound_due,priority:1"`
	Cursor            string `json:"-" gorm:"type:text"`
	Attempt           int    `json:"attempt" gorm:"not null;default:0"`
	NextAttemptAt     int64  `json:"next_attempt_at" gorm:"bigint;not null;default:0;index:idx_assistant_inbound_due,priority:2"`
	ClaimOwner        string `json:"-" gorm:"type:varchar(191);not null;default:'';index"`
	LeaseUntil        int64  `json:"-" gorm:"bigint;not null;default:0;index"`
	ErrorCode         string `json:"error_code,omitempty" gorm:"type:varchar(128)"`
	ReceivedAt        int64  `json:"received_at" gorm:"bigint;not null;index"`
	ProcessedAt       int64  `json:"processed_at" gorm:"bigint;not null;default:0"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantInboundEvent) TableName() string { return "assistant_inbox" }

func (event *AssistantInboundEvent) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if event.Status == "" {
		event.Status = AssistantInboundStatusPending
	}
	if event.ReceivedAt == 0 {
		event.ReceivedAt = now
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = now
	}
	event.UpdatedAt = now
	return nil
}

func (event *AssistantInboundEvent) BeforeUpdate(_ *gorm.DB) error {
	event.UpdatedAt = common.GetTimestamp()
	return nil
}
