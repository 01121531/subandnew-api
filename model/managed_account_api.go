package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	ManagedAccountAPIEnabled  = "enabled"
	ManagedAccountAPIDisabled = "disabled"
)

type ManagedAccountAPI struct {
	ID                 int64          `json:"id" gorm:"primaryKey"`
	Name               string         `json:"name" gorm:"type:varchar(96);not null;index"`
	Description        string         `json:"description" gorm:"type:varchar(500)"`
	Status             string         `json:"status" gorm:"type:varchar(16);not null;default:'enabled';index"`
	Dataset            string         `json:"dataset" gorm:"type:varchar(32);not null;index"`
	PresetDays         int            `json:"preset_days" gorm:"not null;default:7"`
	Timezone           string         `json:"timezone" gorm:"type:varchar(64);not null;default:'Asia/Shanghai'"`
	IncludeTerms       string         `json:"-" gorm:"type:text;not null"`
	ExcludeTerms       string         `json:"-" gorm:"type:text;not null"`
	MatchMode          string         `json:"match_mode" gorm:"type:varchar(8);not null;default:'all'"`
	Rules              string         `json:"-" gorm:"type:text;not null"`
	Fields             string         `json:"-" gorm:"type:text;not null"`
	SortBy             string         `json:"sort_by" gorm:"type:varchar(32);not null;default:'created_at'"`
	SortOrder          string         `json:"sort_order" gorm:"type:varchar(8);not null;default:'desc'"`
	PageSize           int            `json:"page_size" gorm:"not null;default:50"`
	RateLimitPerMinute int            `json:"rate_limit_per_minute" gorm:"not null;default:60"`
	AllowedCIDRs       string         `json:"-" gorm:"type:text;not null"`
	PortalEnabled      bool           `json:"portal_enabled" gorm:"not null;default:false;index"`
	PortalSlug         *string        `json:"-" gorm:"type:varchar(48);uniqueIndex"`
	PortalPasswordHash string         `json:"-" gorm:"type:varchar(100)"`
	PortalPasswordAt   int64          `json:"portal_password_at" gorm:"bigint;not null;default:0"`
	MatchedCount       int            `json:"matched_count" gorm:"not null;default:0"`
	LastObservedAt     int64          `json:"last_observed_at" gorm:"bigint;not null;default:0"`
	LastAccessedAt     int64          `json:"last_accessed_at" gorm:"bigint;not null;default:0;index"`
	RequestCount       int64          `json:"request_count" gorm:"bigint;not null;default:0"`
	CreatedBy          int            `json:"created_by" gorm:"not null;index"`
	UpdatedBy          int            `json:"updated_by" gorm:"not null;index"`
	CreatedAt          int64          `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt          int64          `json:"updated_at" gorm:"bigint;not null;index"`
	DeletedAt          gorm.DeletedAt `json:"-" gorm:"index"`
}

func (ManagedAccountAPI) TableName() string { return "managed_account_apis" }

func (entry *ManagedAccountAPI) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if entry.CreatedAt == 0 {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	return nil
}

func (entry *ManagedAccountAPI) BeforeUpdate(_ *gorm.DB) error {
	entry.UpdatedAt = common.GetTimestamp()
	return nil
}

type ManagedAccountAPIInstance struct {
	ID         int64 `json:"id" gorm:"primaryKey"`
	APIID      int64 `json:"api_id" gorm:"not null;index;uniqueIndex:uidx_managed_account_api_instance"`
	InstanceID int64 `json:"instance_id" gorm:"not null;index;uniqueIndex:uidx_managed_account_api_instance"`
}

func (ManagedAccountAPIInstance) TableName() string { return "managed_account_api_instances" }

type ManagedAccountAPIKey struct {
	ID         int64  `json:"id" gorm:"primaryKey"`
	APIID      int64  `json:"api_id" gorm:"not null;index"`
	Name       string `json:"name" gorm:"type:varchar(64);not null"`
	Prefix     string `json:"prefix" gorm:"type:varchar(24);not null;uniqueIndex"`
	SecretHash string `json:"-" gorm:"type:char(64);not null"`
	ExpiresAt  int64  `json:"expires_at" gorm:"bigint;not null;index"`
	RevokedAt  int64  `json:"revoked_at" gorm:"bigint;not null;default:0;index"`
	LastUsedAt int64  `json:"last_used_at" gorm:"bigint;not null;default:0"`
	CreatedBy  int    `json:"created_by" gorm:"not null;index"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (ManagedAccountAPIKey) TableName() string { return "managed_account_api_keys" }

func (key *ManagedAccountAPIKey) BeforeCreate(_ *gorm.DB) error {
	if key.CreatedAt == 0 {
		key.CreatedAt = common.GetTimestamp()
	}
	return nil
}

type ManagedAccountAPIAccessLog struct {
	ID          int64  `json:"id" gorm:"primaryKey"`
	APIID       int64  `json:"api_id" gorm:"not null;index"`
	KeyID       int64  `json:"key_id" gorm:"not null;index"`
	KeyPrefix   string `json:"key_prefix" gorm:"type:varchar(24);not null"`
	AuthType    string `json:"auth_type" gorm:"type:varchar(16);not null;default:'api_key';index"`
	Action      string `json:"action" gorm:"type:varchar(24);not null;default:'query';index"`
	SessionID   int64  `json:"session_id" gorm:"not null;default:0;index"`
	RequestID   string `json:"request_id" gorm:"type:varchar(64);not null;index"`
	IPAddress   string `json:"ip_address" gorm:"type:varchar(64);not null;index"`
	StatusCode  int    `json:"status_code" gorm:"not null;index"`
	DurationMS  int64  `json:"duration_ms" gorm:"bigint;not null"`
	ResultCount int    `json:"result_count" gorm:"not null;default:0"`
	ErrorCode   string `json:"error_code" gorm:"type:varchar(64);index"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (ManagedAccountAPIAccessLog) TableName() string { return "managed_account_api_access_logs" }

func (entry *ManagedAccountAPIAccessLog) BeforeCreate(_ *gorm.DB) error {
	if entry.CreatedAt == 0 {
		entry.CreatedAt = common.GetTimestamp()
	}
	return nil
}

type ManagedAccountAPIPortalSession struct {
	ID         int64  `json:"id" gorm:"primaryKey"`
	APIID      int64  `json:"api_id" gorm:"not null;index"`
	TokenHash  string `json:"-" gorm:"type:char(64);not null;uniqueIndex"`
	CSRFHash   string `json:"-" gorm:"type:char(64);not null"`
	IPAddress  string `json:"ip_address" gorm:"type:varchar(64);not null"`
	ExpiresAt  int64  `json:"expires_at" gorm:"bigint;not null;index"`
	LastUsedAt int64  `json:"last_used_at" gorm:"bigint;not null;default:0"`
	RevokedAt  int64  `json:"revoked_at" gorm:"bigint;not null;default:0;index"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (ManagedAccountAPIPortalSession) TableName() string {
	return "managed_account_api_portal_sessions"
}

func (session *ManagedAccountAPIPortalSession) BeforeCreate(_ *gorm.DB) error {
	if session.CreatedAt == 0 {
		session.CreatedAt = common.GetTimestamp()
	}
	if session.LastUsedAt == 0 {
		session.LastUsedAt = session.CreatedAt
	}
	return nil
}
