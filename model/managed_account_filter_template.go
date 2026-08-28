package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

type ManagedAccountFilterTemplate struct {
	Id        int64  `json:"id" gorm:"primaryKey"`
	ActorID   int    `json:"actor_id" gorm:"not null;uniqueIndex:uidx_managed_account_filter_template_actor_name,priority:1;index"`
	Name      string `json:"name" gorm:"type:varchar(64);not null;uniqueIndex:uidx_managed_account_filter_template_actor_name,priority:2"`
	MatchMode string `json:"match_mode" gorm:"type:varchar(8);not null"`
	Rules     string `json:"-" gorm:"type:text;not null"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedAccountFilterTemplate) TableName() string {
	return "managed_account_filter_templates"
}

func (template *ManagedAccountFilterTemplate) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if template.CreatedAt == 0 {
		template.CreatedAt = now
	}
	template.UpdatedAt = now
	return nil
}

func (template *ManagedAccountFilterTemplate) BeforeUpdate(_ *gorm.DB) error {
	template.UpdatedAt = common.GetTimestamp()
	return nil
}
