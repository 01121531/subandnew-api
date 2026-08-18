package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

// ManagedDashboardSnapshot stores the latest successful dashboard payload for
// one instance and one normalized time range. Failed attempts update only the
// attempt fields so readers can continue serving the last successful payload.
type ManagedDashboardSnapshot struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	InstanceID        int64  `json:"instance_id" gorm:"not null;index;uniqueIndex:uidx_managed_dashboard_snapshot"`
	RangeKey          string `json:"range_key" gorm:"type:varchar(96);not null;index;uniqueIndex:uidx_managed_dashboard_snapshot"`
	PresetDays        int    `json:"preset_days" gorm:"not null;default:0"`
	WindowStart       int64  `json:"window_start" gorm:"bigint;not null;default:0"`
	WindowEnd         int64  `json:"window_end" gorm:"bigint;not null;default:0"`
	Timezone          string `json:"timezone" gorm:"type:varchar(64);not null;default:'Asia/Shanghai'"`
	SchemaVersion     int    `json:"schema_version" gorm:"not null;default:1"`
	ObservedAt        int64  `json:"observed_at" gorm:"bigint;not null;default:0;index"`
	ETag              string `json:"etag" gorm:"column:etag;type:varchar(64)"`
	Payload           string `json:"-" gorm:"type:text;not null"`
	LastAttemptAt     int64  `json:"last_attempt_at" gorm:"bigint;not null;default:0;index"`
	LastAttemptStatus string `json:"last_attempt_status" gorm:"type:varchar(32);not null;default:'';index"`
	LastErrorCode     string `json:"last_error_code" gorm:"type:varchar(64)"`
	LastAccessedAt    int64  `json:"last_accessed_at" gorm:"bigint;not null;default:0;index"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedDashboardSnapshot) TableName() string { return "managed_dashboard_snapshots" }

func (snapshot *ManagedDashboardSnapshot) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if snapshot.SchemaVersion == 0 {
		snapshot.SchemaVersion = 1
	}
	if snapshot.Timezone == "" {
		snapshot.Timezone = "Asia/Shanghai"
	}
	if snapshot.LastAccessedAt == 0 {
		snapshot.LastAccessedAt = now
	}
	if snapshot.CreatedAt == 0 {
		snapshot.CreatedAt = now
	}
	snapshot.UpdatedAt = now
	return nil
}

func (snapshot *ManagedDashboardSnapshot) BeforeUpdate(_ *gorm.DB) error {
	snapshot.UpdatedAt = common.GetTimestamp()
	return nil
}
