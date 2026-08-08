package model

import (
	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
)

const (
	ManagedInstanceAlertTypeAvailability = "availability"
	ManagedInstanceAlertTypeCredential   = "credential"

	ManagedInstanceAlertStatusOpen     = "open"
	ManagedInstanceAlertStatusResolved = "resolved"
)

type ManagedInstanceAlert struct {
	Id          int64  `json:"id" gorm:"primaryKey"`
	InstanceId  int64  `json:"instance_id" gorm:"not null;index"`
	AlertType   string `json:"alert_type" gorm:"type:varchar(32);not null;index"`
	Status      string `json:"status" gorm:"type:varchar(16);not null;index"`
	ErrorCode   string `json:"error_code" gorm:"type:varchar(64);not null"`
	Occurrences int    `json:"occurrences" gorm:"not null;default:1"`
	FirstSeenAt int64  `json:"first_seen_at" gorm:"bigint;not null;index"`
	LastSeenAt  int64  `json:"last_seen_at" gorm:"bigint;not null;index"`
	ResolvedAt  int64  `json:"resolved_at" gorm:"bigint;not null;default:0;index"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedInstanceAlert) TableName() string { return "managed_instance_alerts" }

func (alert *ManagedInstanceAlert) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if alert.Status == "" {
		alert.Status = ManagedInstanceAlertStatusOpen
	}
	if alert.Occurrences == 0 {
		alert.Occurrences = 1
	}
	if alert.FirstSeenAt == 0 {
		alert.FirstSeenAt = now
	}
	if alert.LastSeenAt == 0 {
		alert.LastSeenAt = now
	}
	if alert.CreatedAt == 0 {
		alert.CreatedAt = now
	}
	alert.UpdatedAt = now
	return nil
}

func (alert *ManagedInstanceAlert) BeforeUpdate(_ *gorm.DB) error {
	alert.UpdatedAt = common.GetTimestamp()
	return nil
}
