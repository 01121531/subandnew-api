package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

// AssistantChannelSecret stores encrypted iLink login/session credentials.
// The ciphertext is bound to the channel ID by the assistant secret cipher.
type AssistantChannelSecret struct {
	ID          int64  `json:"id" gorm:"primaryKey"`
	ChannelID   int64  `json:"channel_id" gorm:"not null;uniqueIndex"`
	Ciphertext  string `json:"-" gorm:"type:text;not null"`
	KeyVersion  string `json:"-" gorm:"type:varchar(50);not null"`
	Fingerprint string `json:"fingerprint" gorm:"type:char(64);not null"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantChannelSecret) TableName() string { return "assistant_channel_secrets" }

func (secret *AssistantChannelSecret) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if secret.CreatedAt == 0 {
		secret.CreatedAt = now
	}
	secret.UpdatedAt = now
	return nil
}

func (secret *AssistantChannelSecret) BeforeUpdate(_ *gorm.DB) error {
	secret.UpdatedAt = common.GetTimestamp()
	return nil
}
