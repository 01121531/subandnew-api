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
	ID               int64  `json:"id" gorm:"primaryKey"`
	ChannelID        int64  `json:"channel_id" gorm:"not null;uniqueIndex:uidx_assistant_conversation_peer,priority:1;index"`
	AccountID        string `json:"account_id" gorm:"type:varchar(191);not null;uniqueIndex:uidx_assistant_conversation_peer,priority:2"`
	PeerID           string `json:"peer_id" gorm:"type:varchar(191);not null;uniqueIndex:uidx_assistant_conversation_peer,priority:3"`
	UserID           int    `json:"user_id" gorm:"not null;index"`
	Status           string `json:"status" gorm:"type:varchar(32);not null;default:'active';index"`
	Summary          string `json:"-" gorm:"type:text"`
	ScopeFingerprint string `json:"-" gorm:"type:char(64);not null;default:'';index"`
	LastMessageAt    int64  `json:"last_message_at" gorm:"bigint;not null;default:0;index"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

// AssistantConversationLease serializes one peer's turns across worker nodes.
type AssistantConversationLease struct {
	ID           int64  `json:"id" gorm:"primaryKey"`
	ChannelID    int64  `json:"channel_id" gorm:"not null;uniqueIndex:uidx_assistant_conversation_lease,priority:1"`
	AccountID    string `json:"account_id" gorm:"type:varchar(191);not null"`
	PeerID       string `json:"peer_id" gorm:"type:varchar(191);not null;uniqueIndex:uidx_assistant_conversation_lease,priority:2"`
	OwnerID      string `json:"-" gorm:"type:varchar(191);not null;default:'';index"`
	LockedUntil  int64  `json:"-" gorm:"bigint;not null;default:0;index"`
	FencingToken int64  `json:"-" gorm:"bigint;not null;default:0"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantConversationLease) TableName() string { return "assistant_conversation_leases" }

func (lease *AssistantConversationLease) BeforeCreate(_ *gorm.DB) error {
	lease.UpdatedAt = common.GetTimestamp()
	return nil
}

func (lease *AssistantConversationLease) BeforeUpdate(_ *gorm.DB) error {
	lease.UpdatedAt = common.GetTimestamp()
	return nil
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
