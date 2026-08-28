package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	AssistantMessageRoleUser      = "user"
	AssistantMessageRoleAssistant = "assistant"
)

// AssistantMessage stores short-term conversation memory encrypted at rest.
// Content and its key metadata are never exposed through JSON APIs.
type AssistantMessage struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	ConversationID    int64  `json:"conversation_id" gorm:"not null;index"`
	InboundEventID    int64  `json:"inbound_event_id" gorm:"not null;uniqueIndex:uidx_assistant_message_turn,priority:1;index"`
	RunID             int64  `json:"run_id,omitempty" gorm:"not null;default:0;index"`
	Role              string `json:"role" gorm:"type:varchar(16);not null;uniqueIndex:uidx_assistant_message_turn,priority:2"`
	Content           string `json:"-" gorm:"type:text;not null"`
	ContentKeyVersion string `json:"-" gorm:"type:varchar(32);not null"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (AssistantMessage) TableName() string { return "assistant_messages" }

func (message *AssistantMessage) BeforeCreate(_ *gorm.DB) error {
	if message.CreatedAt == 0 {
		message.CreatedAt = common.GetTimestamp()
	}
	return nil
}
