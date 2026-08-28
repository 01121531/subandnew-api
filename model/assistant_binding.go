package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

// AssistantBindingCode is a short-lived, single-use bridge from an
// authenticated web user to one external channel identity. Only a hash of the
// user-visible code is stored.
type AssistantBindingCode struct {
	ID                   int64  `json:"id" gorm:"primaryKey"`
	CodeHash             string `json:"-" gorm:"type:char(64);not null;uniqueIndex"`
	UserID               int    `json:"user_id" gorm:"not null;index"`
	AllowedInstanceScope string `json:"allowed_instance_scope" gorm:"type:varchar(16);not null;default:'none'"`
	InstanceIDs          string `json:"-" gorm:"type:text;not null"`
	ExpiresAt            int64  `json:"expires_at" gorm:"bigint;not null;index"`
	ConsumedAt           int64  `json:"consumed_at" gorm:"bigint;not null;default:0;index"`
	ConsumedByIdentityID int64  `json:"consumed_by_identity_id" gorm:"bigint;not null;default:0"`
	CreatedBy            int    `json:"created_by" gorm:"not null;index"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantBindingCode) TableName() string { return "assistant_binding_codes" }

func (binding *AssistantBindingCode) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if binding.AllowedInstanceScope == "" {
		binding.AllowedInstanceScope = AssistantInstanceScopeNone
	}
	if binding.InstanceIDs == "" {
		binding.InstanceIDs = "[]"
	}
	if binding.CreatedAt == 0 {
		binding.CreatedAt = now
	}
	binding.UpdatedAt = now
	return nil
}

func (binding *AssistantBindingCode) BeforeUpdate(_ *gorm.DB) error {
	binding.UpdatedAt = common.GetTimestamp()
	return nil
}
