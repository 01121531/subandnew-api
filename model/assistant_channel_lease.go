package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

// AssistantChannelLease guarantees that one process polls a connected channel
// at a time. FencingToken changes on every successful acquisition/renewal.
type AssistantChannelLease struct {
	ChannelID    int64  `json:"channel_id" gorm:"primaryKey"`
	OwnerID      string `json:"owner_id" gorm:"type:varchar(191);not null;index"`
	FencingToken int64  `json:"fencing_token" gorm:"bigint;not null;default:0"`
	LockedUntil  int64  `json:"locked_until" gorm:"bigint;not null;default:0;index"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantChannelLease) TableName() string { return "assistant_channel_leases" }

func (lease *AssistantChannelLease) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if lease.CreatedAt == 0 {
		lease.CreatedAt = now
	}
	lease.UpdatedAt = now
	return nil
}

func (lease *AssistantChannelLease) BeforeUpdate(_ *gorm.DB) error {
	lease.UpdatedAt = common.GetTimestamp()
	return nil
}
