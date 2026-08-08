package model

import (
	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
)

const (
	ManagedInstanceSnapshotTypeInventory = "inventory"
	ManagedInstanceSnapshotTypeSummary   = "summary"

	ManagedInstanceCollectionSucceeded   = "succeeded"
	ManagedInstanceCollectionFailed      = "failed"
	ManagedInstanceCollectionUnsupported = "unsupported"
)

// ManagedInstanceSnapshot stores only the latest normalized observation for a
// snapshot type and resource kind. Payloads must contain no remote secrets or
// user-private fields.
type ManagedInstanceSnapshot struct {
	Id               int64  `json:"id" gorm:"primaryKey"`
	InstanceId       int64  `json:"instance_id" gorm:"not null;index;uniqueIndex:uidx_managed_instance_snapshot_latest"`
	SnapshotType     string `json:"snapshot_type" gorm:"type:varchar(32);not null;index;uniqueIndex:uidx_managed_instance_snapshot_latest"`
	ResourceKind     string `json:"resource_kind" gorm:"type:varchar(32);not null;default:'';uniqueIndex:uidx_managed_instance_snapshot_latest"`
	SchemaVersion    int    `json:"schema_version" gorm:"not null;default:1"`
	ObservedAt       int64  `json:"observed_at" gorm:"bigint;not null;index"`
	ETag             string `json:"etag,omitempty" gorm:"column:etag;type:varchar(64)"`
	Payload          string `json:"-" gorm:"type:text;not null"`
	CollectionStatus string `json:"collection_status" gorm:"type:varchar(32);not null;index"`
	ErrorCode        string `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedInstanceSnapshot) TableName() string { return "managed_instance_snapshots" }

func (snapshot *ManagedInstanceSnapshot) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if snapshot.SchemaVersion == 0 {
		snapshot.SchemaVersion = 1
	}
	if snapshot.ObservedAt == 0 {
		snapshot.ObservedAt = now
	}
	if snapshot.CreatedAt == 0 {
		snapshot.CreatedAt = now
	}
	snapshot.UpdatedAt = now
	return nil
}

func (snapshot *ManagedInstanceSnapshot) BeforeUpdate(_ *gorm.DB) error {
	snapshot.UpdatedAt = common.GetTimestamp()
	return nil
}
