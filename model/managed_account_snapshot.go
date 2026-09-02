package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	ManagedAccountSnapshotKindInventory = "inventory"
	ManagedAccountSnapshotKindOutput    = "account_output"
)

// ManagedAccountSnapshot keeps the latest successful account-management
// payload while recording failed refresh attempts separately.
type ManagedAccountSnapshot struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	InstanceID        int64  `json:"instance_id" gorm:"not null;index;uniqueIndex:uidx_managed_account_snapshot"`
	SnapshotKind      string `json:"snapshot_kind" gorm:"type:varchar(32);not null;index;uniqueIndex:uidx_managed_account_snapshot"`
	RangeKey          string `json:"range_key" gorm:"type:varchar(64);not null;default:'';uniqueIndex:uidx_managed_account_snapshot"`
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

func (ManagedAccountSnapshot) TableName() string { return "managed_account_snapshots" }

// ManagedAccountDailySnapshot is the immutable first successful standard
// account snapshot captured on each Asia/Shanghai natural day. The latest
// snapshot table remains compact while this table preserves daily baselines.
type ManagedAccountDailySnapshot struct {
	ID            int64  `json:"id" gorm:"primaryKey"`
	InstanceID    int64  `json:"instance_id" gorm:"not null;index;uniqueIndex:uidx_managed_account_daily_snapshot"`
	SnapshotKind  string `json:"snapshot_kind" gorm:"type:varchar(32);not null;index;uniqueIndex:uidx_managed_account_daily_snapshot"`
	RangeKey      string `json:"range_key" gorm:"type:varchar(64);not null;default:'';uniqueIndex:uidx_managed_account_daily_snapshot"`
	SnapshotDate  string `json:"snapshot_date" gorm:"type:varchar(10);not null;index;uniqueIndex:uidx_managed_account_daily_snapshot"`
	BoundaryAt    int64  `json:"boundary_at" gorm:"bigint;not null;index"`
	PresetDays    int    `json:"preset_days" gorm:"not null;default:0"`
	WindowStart   int64  `json:"window_start" gorm:"bigint;not null;default:0"`
	WindowEnd     int64  `json:"window_end" gorm:"bigint;not null;default:0"`
	Timezone      string `json:"timezone" gorm:"type:varchar(64);not null;default:'Asia/Shanghai'"`
	SchemaVersion int    `json:"schema_version" gorm:"not null;default:1"`
	ObservedAt    int64  `json:"observed_at" gorm:"bigint;not null;index"`
	CapturedAt    int64  `json:"captured_at" gorm:"bigint;not null;index"`
	ETag          string `json:"etag" gorm:"column:etag;type:varchar(64)"`
	Payload       string `json:"-" gorm:"type:text;not null"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;not null"`
}

func (ManagedAccountDailySnapshot) TableName() string {
	return "managed_account_daily_snapshots"
}

func (snapshot *ManagedAccountDailySnapshot) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if snapshot.SchemaVersion == 0 {
		snapshot.SchemaVersion = 1
	}
	if snapshot.Timezone == "" {
		snapshot.Timezone = "Asia/Shanghai"
	}
	if snapshot.CapturedAt == 0 {
		snapshot.CapturedAt = now
	}
	if snapshot.CreatedAt == 0 {
		snapshot.CreatedAt = now
	}
	return nil
}

func (snapshot *ManagedAccountSnapshot) BeforeCreate(_ *gorm.DB) error {
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

func (snapshot *ManagedAccountSnapshot) BeforeUpdate(_ *gorm.DB) error {
	snapshot.UpdatedAt = common.GetTimestamp()
	return nil
}
