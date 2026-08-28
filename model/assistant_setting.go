package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

// AssistantSetting stores singleton assistant-wide preferences.
type AssistantSetting struct {
	ID                      int64  `json:"id" gorm:"primaryKey"`
	GlobalDefaultInstanceID *int64 `json:"global_default_instance_id" gorm:"index"`
	UpdatedBy               int    `json:"updated_by" gorm:"not null;default:0;index"`
	CreatedAt               int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt               int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantSetting) TableName() string { return "assistant_settings" }

func (setting *AssistantSetting) BeforeCreate(_ *gorm.DB) error {
	if setting.ID == 0 {
		setting.ID = 1
	}
	now := common.GetTimestamp()
	if setting.CreatedAt == 0 {
		setting.CreatedAt = now
	}
	setting.UpdatedAt = now
	return nil
}

func (setting *AssistantSetting) BeforeUpdate(_ *gorm.DB) error {
	setting.UpdatedAt = common.GetTimestamp()
	return nil
}
