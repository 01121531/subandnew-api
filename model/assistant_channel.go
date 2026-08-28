package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	AssistantChannelTypeWechatILink = "wechat_ilink"

	AssistantChannelStatusUnbound        = "unbound"
	AssistantChannelStatusQRIssued       = "qr_issued"
	AssistantChannelStatusScanned        = "scanned"
	AssistantChannelStatusVerifyRequired = "verify_required"
	AssistantChannelStatusConnected      = "connected"
	AssistantChannelStatusDegraded       = "degraded"
	AssistantChannelStatusReauthRequired = "reauth_required"

	AssistantIdentityStatusPending  = "pending"
	AssistantIdentityStatusActive   = "active"
	AssistantIdentityStatusDisabled = "disabled"
	AssistantIdentityStatusRevoked  = "revoked"

	AssistantInstanceScopeNone     = "none"
	AssistantInstanceScopeSelected = "selected"
	AssistantInstanceScopeAll      = "all"
)

// AssistantChannel is one externally addressable assistant account. Config may
// contain protocol-specific connection metadata, so it is never serialized.
// Authentication secrets belong in the separate encrypted secret store.
type AssistantChannel struct {
	ID           int64  `json:"id" gorm:"primaryKey"`
	Type         string `json:"type" gorm:"type:varchar(32);not null;uniqueIndex:uidx_assistant_channel_account,priority:1;index"`
	AccountID    string `json:"account_id" gorm:"type:varchar(191);not null;uniqueIndex:uidx_assistant_channel_account,priority:2"`
	Status       string `json:"status" gorm:"type:varchar(32);not null;default:'unbound';index"`
	Enabled      bool   `json:"enabled" gorm:"not null;default:false;index"`
	Config       string `json:"-" gorm:"type:text;not null"`
	LastSeenAt   int64  `json:"last_seen_at" gorm:"bigint;not null;default:0;index"`
	ReauthReason string `json:"reauth_reason,omitempty" gorm:"type:varchar(255)"`
	CreatedBy    int    `json:"created_by" gorm:"not null;default:0;index"`
	UpdatedBy    int    `json:"updated_by" gorm:"not null;default:0"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantChannel) TableName() string { return "assistant_channels" }

func (channel *AssistantChannel) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if channel.Status == "" {
		channel.Status = AssistantChannelStatusUnbound
	}
	if channel.Config == "" {
		channel.Config = "{}"
	}
	if channel.CreatedAt == 0 {
		channel.CreatedAt = now
	}
	channel.UpdatedAt = now
	return nil
}

func (channel *AssistantChannel) BeforeUpdate(_ *gorm.DB) error {
	channel.UpdatedAt = common.GetTimestamp()
	return nil
}

// AssistantIdentity binds one channel-local external user to one local user.
// AllowedInstanceScope fails closed; selected instances are normalized into
// AssistantIdentityInstanceScope rows so authorization can be queried safely.
type AssistantIdentity struct {
	ID                   int64  `json:"id" gorm:"primaryKey"`
	ChannelID            int64  `json:"channel_id" gorm:"not null;uniqueIndex:uidx_assistant_identity_external,priority:1;index"`
	ExternalUserID       string `json:"external_user_id" gorm:"type:varchar(191);not null;uniqueIndex:uidx_assistant_identity_external,priority:2"`
	UserID               int    `json:"user_id" gorm:"not null;index"`
	Status               string `json:"status" gorm:"type:varchar(32);not null;default:'pending';index"`
	AllowedInstanceScope string `json:"allowed_instance_scope" gorm:"type:varchar(16);not null;default:'none';index"`
	DefaultInstanceID    *int64 `json:"default_instance_id" gorm:"index"`
	BoundBy              int    `json:"bound_by" gorm:"not null;default:0;index"`
	BoundAt              int64  `json:"bound_at" gorm:"bigint;not null;default:0"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (AssistantIdentity) TableName() string { return "assistant_identities" }

func (identity *AssistantIdentity) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if identity.Status == "" {
		identity.Status = AssistantIdentityStatusPending
	}
	if identity.AllowedInstanceScope == "" {
		identity.AllowedInstanceScope = AssistantInstanceScopeNone
	}
	if identity.CreatedAt == 0 {
		identity.CreatedAt = now
	}
	identity.UpdatedAt = now
	return nil
}

func (identity *AssistantIdentity) BeforeUpdate(_ *gorm.DB) error {
	identity.UpdatedAt = common.GetTimestamp()
	return nil
}

// AssistantIdentityInstanceScope is an allow-list entry used only when an
// identity's AllowedInstanceScope is "selected".
type AssistantIdentityInstanceScope struct {
	ID         int64 `json:"id" gorm:"primaryKey"`
	IdentityID int64 `json:"identity_id" gorm:"not null;uniqueIndex:uidx_assistant_identity_instance,priority:1;index"`
	InstanceID int64 `json:"instance_id" gorm:"not null;uniqueIndex:uidx_assistant_identity_instance,priority:2;index"`
	CreatedBy  int   `json:"created_by" gorm:"not null;default:0;index"`
	CreatedAt  int64 `json:"created_at" gorm:"bigint;not null;index"`
}

func (AssistantIdentityInstanceScope) TableName() string {
	return "assistant_identity_instance_scopes"
}

func (scope *AssistantIdentityInstanceScope) BeforeCreate(_ *gorm.DB) error {
	if scope.CreatedAt == 0 {
		scope.CreatedAt = common.GetTimestamp()
	}
	return nil
}
