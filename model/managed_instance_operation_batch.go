package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	ManagedInstanceBatchStatusPlanning         = "planning"
	ManagedInstanceBatchStatusPlanned          = "planned"
	ManagedInstanceBatchStatusPartiallyPlanned = "partially_planned"
	ManagedInstanceBatchStatusQueued           = "queued"
	ManagedInstanceBatchStatusRunning          = "running"
	ManagedInstanceBatchStatusSucceeded        = "succeeded"
	ManagedInstanceBatchStatusPartiallyFailed  = "partially_failed"
	ManagedInstanceBatchStatusFailed           = "failed"
	ManagedInstanceBatchStatusNeedsReconcile   = "needs_reconcile"
)

// ManagedInstanceOperationBatch is the durable parent for a collection of
// existing two-phase managed-instance operations.
type ManagedInstanceOperationBatch struct {
	Id                     int64  `json:"id" gorm:"primaryKey"`
	BatchId                string `json:"batch_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	ActorId                int    `json:"actor_id" gorm:"not null;index;uniqueIndex:uidx_managed_instance_batch_idempotency"`
	ExecutedBy             int    `json:"executed_by" gorm:"not null;default:0;index"`
	Action                 string `json:"action" gorm:"type:varchar(32);not null;index"`
	Status                 string `json:"status" gorm:"type:varchar(32);not null;index"`
	TargetCount            int    `json:"target_count" gorm:"not null"`
	IdempotencyKey         string `json:"-" gorm:"type:varchar(64);not null;uniqueIndex:uidx_managed_instance_batch_idempotency"`
	IdempotencyFingerprint string `json:"idempotency_fingerprint" gorm:"type:varchar(16);not null"`
	PlanHash               string `json:"-" gorm:"type:varchar(64);not null"`
	PlannedAt              int64  `json:"planned_at" gorm:"bigint;not null;index"`
	ExecutedAt             int64  `json:"executed_at" gorm:"bigint;not null;default:0"`
	FinishedAt             int64  `json:"finished_at" gorm:"bigint;not null;default:0"`
	CreatedAt              int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt              int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedInstanceOperationBatch) TableName() string {
	return "managed_instance_operation_batches"
}

func (batch *ManagedInstanceOperationBatch) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if batch.Status == "" {
		batch.Status = ManagedInstanceBatchStatusPlanned
	}
	if batch.PlannedAt == 0 {
		batch.PlannedAt = now
	}
	if batch.CreatedAt == 0 {
		batch.CreatedAt = now
	}
	batch.UpdatedAt = now
	return nil
}

func (batch *ManagedInstanceOperationBatch) BeforeUpdate(_ *gorm.DB) error {
	batch.UpdatedAt = common.GetTimestamp()
	return nil
}

// ManagedInstanceOperationBatchItem links one target to its child operation.
// Parameters are control-plane resource IDs only and are never credentials.
type ManagedInstanceOperationBatchItem struct {
	Id          int64  `json:"id" gorm:"primaryKey"`
	BatchId     string `json:"batch_id" gorm:"type:varchar(64);not null;index;uniqueIndex:uidx_managed_instance_batch_target"`
	InstanceId  int64  `json:"instance_id" gorm:"not null;index;uniqueIndex:uidx_managed_instance_batch_target"`
	OperationId string `json:"operation_id,omitempty" gorm:"type:varchar(64);index"`
	Position    int    `json:"position" gorm:"not null"`
	Status      string `json:"status" gorm:"type:varchar(32);not null;index"`
	ErrorCode   string `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	Parameters  string `json:"-" gorm:"type:text;not null"`
	CreatedAt   int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (ManagedInstanceOperationBatchItem) TableName() string {
	return "managed_instance_operation_batch_items"
}

func (item *ManagedInstanceOperationBatchItem) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if item.CreatedAt == 0 {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	return nil
}

func (item *ManagedInstanceOperationBatchItem) BeforeUpdate(_ *gorm.DB) error {
	item.UpdatedAt = common.GetTimestamp()
	return nil
}
