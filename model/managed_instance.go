package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	ManagedInstanceKindNewAPI    = "new_api"
	ManagedInstanceKindHuichuan  = "huichuan"
	ManagedInstanceKindSub2API   = "sub2api"
	ManagedInstanceKindConductor = "conductor"
	ManagedInstanceKindGeneric   = "generic"

	ManagedInstanceModeObserve = "observe"
	ManagedInstanceModeOperate = "operate"
	ManagedInstanceModeEnforce = "enforce"

	ManagedInstanceStatusUnknown    = "unknown"
	ManagedInstanceStatusHealthy    = "healthy"
	ManagedInstanceStatusDegraded   = "degraded"
	ManagedInstanceStatusOffline    = "offline"
	ManagedInstanceStatusAuthFailed = "auth_failed"

	ManagedInstanceAccessAdmin = "admin"
	ManagedInstanceAccessUser  = "user"
)

type ManagedInstance struct {
	Id                    int64  `json:"id" gorm:"primaryKey"`
	Name                  string `json:"name" gorm:"type:varchar(128);not null;uniqueIndex"`
	Kind                  string `json:"kind" gorm:"type:varchar(32);not null;index"`
	BaseURL               string `json:"base_url" gorm:"type:varchar(512);not null;uniqueIndex"`
	Environment           string `json:"environment" gorm:"type:varchar(32);not null;default:'production';index"`
	Labels                string `json:"-" gorm:"type:text"`
	ManagementMode        string `json:"management_mode" gorm:"type:varchar(32);not null;default:'observe';index"`
	Status                string `json:"status" gorm:"type:varchar(32);not null;default:'unknown';index"`
	Version               string `json:"version" gorm:"type:varchar(64)"`
	Capabilities          string `json:"-" gorm:"type:text"`
	TLSVerify             bool   `json:"tls_verify" gorm:"not null;default:true"`
	RequestTimeoutSeconds int    `json:"request_timeout_seconds" gorm:"not null;default:10"`
	CheckIntervalSeconds  int    `json:"check_interval_seconds" gorm:"not null;default:60"`
	AlertFailureThreshold int    `json:"alert_failure_threshold" gorm:"not null;default:0"`
	LastSeenAt            int64  `json:"last_seen_at" gorm:"bigint;not null;default:0;index"`
	LastCheckedAt         int64  `json:"last_checked_at" gorm:"bigint;not null;default:0;index"`
	ConsecutiveFailures   int    `json:"consecutive_failures" gorm:"not null;default:0"`
	CreatedBy             int    `json:"created_by" gorm:"not null;default:0;index"`
	UpdatedBy             int    `json:"updated_by" gorm:"not null;default:0"`
	CreatedAt             int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt             int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedInstance) TableName() string { return "managed_instances" }

func (instance *ManagedInstance) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if instance.Environment == "" {
		instance.Environment = "production"
	}
	if instance.ManagementMode == "" {
		instance.ManagementMode = ManagedInstanceModeObserve
	}
	if instance.Status == "" {
		instance.Status = ManagedInstanceStatusUnknown
	}
	if instance.RequestTimeoutSeconds == 0 {
		instance.RequestTimeoutSeconds = 10
	}
	if instance.CheckIntervalSeconds == 0 {
		instance.CheckIntervalSeconds = 60
	}
	if instance.CreatedAt == 0 {
		instance.CreatedAt = now
	}
	instance.UpdatedAt = now
	return nil
}

func (instance *ManagedInstance) BeforeUpdate(_ *gorm.DB) error {
	instance.UpdatedAt = common.GetTimestamp()
	return nil
}

type ManagedInstanceCredential struct {
	Id             int64  `json:"id" gorm:"primaryKey"`
	InstanceId     int64  `json:"instance_id" gorm:"not null;uniqueIndex"`
	AuthType       string `json:"auth_type" gorm:"type:varchar(32);not null"`
	AccessScope    string `json:"access_scope" gorm:"type:varchar(16);not null;default:'admin'"`
	Ciphertext     string `json:"-" gorm:"type:text;not null"`
	KeyVersion     string `json:"key_version" gorm:"type:varchar(32);not null"`
	Fingerprint    string `json:"fingerprint" gorm:"type:varchar(64);not null"`
	ExpiresAt      int64  `json:"expires_at" gorm:"bigint;not null;default:0;index"`
	LastVerifiedAt int64  `json:"last_verified_at" gorm:"bigint;not null;default:0"`
	RotatedBy      int    `json:"rotated_by" gorm:"not null;default:0"`
	RotatedAt      int64  `json:"rotated_at" gorm:"bigint;not null"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt      int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (ManagedInstanceCredential) TableName() string { return "managed_instance_credentials" }

func (credential *ManagedInstanceCredential) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if credential.AccessScope == "" {
		credential.AccessScope = ManagedInstanceAccessAdmin
	}
	if credential.RotatedAt == 0 {
		credential.RotatedAt = now
	}
	if credential.CreatedAt == 0 {
		credential.CreatedAt = now
	}
	credential.UpdatedAt = now
	return nil
}

func (credential *ManagedInstanceCredential) BeforeUpdate(_ *gorm.DB) error {
	credential.UpdatedAt = common.GetTimestamp()
	return nil
}

type ManagedInstanceAudit struct {
	Id         int64  `json:"id" gorm:"primaryKey"`
	InstanceId int64  `json:"instance_id" gorm:"not null;index"`
	ActorId    int    `json:"actor_id" gorm:"not null;index"`
	Action     string `json:"action" gorm:"type:varchar(64);not null;index"`
	Outcome    string `json:"outcome" gorm:"type:varchar(32);not null;index"`
	Details    string `json:"details" gorm:"type:text"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;not null;index"`
}

func (ManagedInstanceAudit) TableName() string { return "managed_instance_audits" }

func (audit *ManagedInstanceAudit) BeforeCreate(_ *gorm.DB) error {
	if audit.CreatedAt == 0 {
		audit.CreatedAt = common.GetTimestamp()
	}
	return nil
}
