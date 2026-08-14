package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

// ManagedRPMHistory stores one rolling aggregate for each instance minute.
type ManagedRPMHistory struct {
	ID          int64   `json:"id" gorm:"primaryKey"`
	InstanceID  int64   `json:"instance_id" gorm:"not null;index;uniqueIndex:uidx_managed_rpm_history_bucket"`
	BucketStart int64   `json:"bucket_start" gorm:"bigint;not null;index;uniqueIndex:uidx_managed_rpm_history_bucket"`
	RPMSum      float64 `json:"rpm_sum" gorm:"not null;default:0"`
	SampleCount int     `json:"sample_count" gorm:"not null;default:0"`
	RPMLast     float64 `json:"rpm_last" gorm:"not null;default:0"`
	CreatedAt   int64   `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt   int64   `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedRPMHistory) TableName() string { return "managed_rpm_history" }

func (history *ManagedRPMHistory) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if history.CreatedAt == 0 {
		history.CreatedAt = now
	}
	history.UpdatedAt = now
	return nil
}

func (history *ManagedRPMHistory) BeforeUpdate(_ *gorm.DB) error {
	history.UpdatedAt = common.GetTimestamp()
	return nil
}
