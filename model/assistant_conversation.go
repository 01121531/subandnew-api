package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	AssistantConversationStatusActive = "active"
	AssistantConversationStatusClosed = "closed"
)

// AssistantConversation is isolated by channel, account and external peer.
// Summary is retained server-side and is not returned by ordinary JSON APIs.
type AssistantConversation struct {
	ID            int64  `json:"id" gorm:"primaryKey"`
	ChannelID     int64  `json:"channel_id" gorm:"not null;uniqueIndex:uidx_assistant_conversation_peer,priority:1;index"`
	AccountID     string `json:"account_id" gorm:"type:varchar(191);not null;uniqueIndex:uidx_assistant_conversation_peer,priority:2"`
	PeerID        string `json:"peer_id" gorm:"type:varchar(191);not null;uniqueIndex:uidx_assistant_conversation_peer,priority:3"`
	UserID        int    `json:"user_id" gorm:"not null;index"`
	Status        string `json:"status" gorm:"type:varchar(32);not null;default:'active';index"`
	Summary       string `json:"-" gorm:"type:text"`
	LastMessageAt int64  `json:"last_message_at" gorm:"bigint;not null;default:0;index"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantConversation) TableName() string { return "assistant_conversations" }

func (conversation *AssistantConversation) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if conversation.Status == "" {
		conversation.Status = AssistantConversationStatusActive
	}
	if conversation.CreatedAt == 0 {
		conversation.CreatedAt = now
	}
	conversation.UpdatedAt = now
	return nil
}

func (conversation *AssistantConversation) BeforeUpdate(_ *gorm.DB) error {
	conversation.UpdatedAt = common.GetTimestamp()
	return nil
}
