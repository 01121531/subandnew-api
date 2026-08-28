package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

// ManagedRPMHistory stores one rolling aggregate for each instance minute.
type ManagedRPMHistory struct {
	ID                     int64   `json:"id" gorm:"primaryKey"`
	InstanceID             int64   `json:"instance_id" gorm:"not null;index;uniqueIndex:uidx_managed_rpm_history_bucket"`
	BucketStart            int64   `json:"bucket_start" gorm:"bigint;not null;index;uniqueIndex:uidx_managed_rpm_history_bucket"`
	RPMSum                 float64 `json:"rpm_sum" gorm:"not null;default:0"`
	SampleCount            int     `json:"sample_count" gorm:"not null;default:0"`
	RPMLast                float64 `json:"rpm_last" gorm:"not null;default:0"`
	CapacitySum            float64 `json:"capacity_sum" gorm:"not null;default:0"`
	CapacitySampleCount    int     `json:"capacity_sample_count" gorm:"not null;default:0"`
	CapacityLast           float64 `json:"capacity_last" gorm:"not null;default:0"`
	SuccessRateWeightedSum float64 `json:"success_rate_weighted_sum" gorm:"not null;default:0"`
	SuccessRateWeightSum   float64 `json:"success_rate_weight_sum" gorm:"not null;default:0"`
	SuccessRateSampleCount int     `json:"success_rate_sample_count" gorm:"not null;default:0"`
	SuccessRateLast        float64 `json:"success_rate_last" gorm:"not null;default:0"`
	AccountsAvailableLast  int     `json:"accounts_available_last" gorm:"not null;default:0"`
	AccountsTotalLast      int     `json:"accounts_total_last" gorm:"not null;default:0"`
	AccountSampleCount     int     `json:"account_sample_count" gorm:"not null;default:0"`
	ConcurrencyUsedLast    float64 `json:"concurrency_used_last" gorm:"not null;default:0"`
	ConcurrencyMaxLast     float64 `json:"concurrency_max_last" gorm:"not null;default:0"`
	ConcurrencySampleCount int     `json:"concurrency_sample_count" gorm:"not null;default:0"`
	ConcurrencyUsedSamples int     `json:"concurrency_used_samples" gorm:"not null;default:0"`
	ConcurrencyMaxSamples  int     `json:"concurrency_max_samples" gorm:"not null;default:0"`
	TodayCostLast          float64 `json:"today_cost_last" gorm:"not null;default:0"`
	TodayCostSampleCount   int     `json:"today_cost_sample_count" gorm:"not null;default:0"`
	ActiveSessionsLast     int     `json:"active_sessions_last" gorm:"not null;default:0"`
	ActiveSessionSamples   int     `json:"active_session_samples" gorm:"not null;default:0"`
	CreatedAt              int64   `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt              int64   `json:"updated_at" gorm:"bigint;not null;index"`
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
