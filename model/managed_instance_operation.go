package model

import (
	"github.com/01121531/HUICHUAN-AI/common"
	"gorm.io/gorm"
)

const (
	ManagedInstanceActionRefreshInventory = "refresh_inventory"
	ManagedInstanceActionTestResources    = "test_resources"
	ManagedInstanceActionToggleResource   = "toggle_resource"

	ManagedInstanceOperationStatusPlanned   = "planned"
	ManagedInstanceOperationStatusQueued    = "queued"
	ManagedInstanceOperationStatusRunning   = "running"
	ManagedInstanceOperationStatusSucceeded = "succeeded"
	ManagedInstanceOperationStatusFailed    = "failed"
	ManagedInstanceOperationStatusUnknown   = "unknown"

	SystemTaskTypeManagedInstanceOperation = "managed_instance_operation"
)

// ManagedInstanceOperation is the durable business record for a two-phase
// remote operation. Secrets and raw remote responses must never be stored here.
type ManagedInstanceOperation struct {
	Id                     int64  `json:"id" gorm:"primaryKey"`
	OperationId            string `json:"operation_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	InstanceId             int64  `json:"instance_id" gorm:"not null;index;uniqueIndex:uidx_managed_instance_operation_idempotency"`
	TaskId                 string `json:"task_id,omitempty" gorm:"type:varchar(64);index"`
	ActorId                int    `json:"actor_id" gorm:"not null;index"`
	ExecutedBy             int    `json:"executed_by" gorm:"not null;default:0;index"`
	Action                 string `json:"action" gorm:"type:varchar(32);not null;index"`
	Status                 string `json:"status" gorm:"type:varchar(32);not null;index"`
	RiskLevel              string `json:"risk_level" gorm:"type:varchar(16);not null;default:'low'"`
	WritesRemote           bool   `json:"writes_remote" gorm:"not null;default:false"`
	RequiredCapability     string `json:"required_capability" gorm:"type:varchar(64);not null"`
	IdempotencyKey         string `json:"-" gorm:"type:varchar(128);not null;uniqueIndex:uidx_managed_instance_operation_idempotency"`
	IdempotencyFingerprint string `json:"idempotency_fingerprint" gorm:"type:varchar(16);not null"`
	PlanHash               string `json:"-" gorm:"type:varchar(64);not null"`
	Parameters             string `json:"-" gorm:"type:text;not null"`
	Plan                   string `json:"-" gorm:"type:text;not null"`
	Result                 string `json:"-" gorm:"type:text"`
	ErrorCode              string `json:"error_code,omitempty" gorm:"type:varchar(64)"`
	PlannedAt              int64  `json:"planned_at" gorm:"bigint;not null;index"`
	ExecutedAt             int64  `json:"executed_at" gorm:"bigint;not null;default:0"`
	FinishedAt             int64  `json:"finished_at" gorm:"bigint;not null;default:0"`
	CreatedAt              int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt              int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedInstanceOperation) TableName() string { return "managed_instance_operations" }

func (operation *ManagedInstanceOperation) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if operation.Status == "" {
		operation.Status = ManagedInstanceOperationStatusPlanned
	}
	if operation.RiskLevel == "" {
		operation.RiskLevel = "low"
	}
	if operation.PlannedAt == 0 {
		operation.PlannedAt = now
	}
	if operation.CreatedAt == 0 {
		operation.CreatedAt = now
	}
	operation.UpdatedAt = now
	return nil
}

func (operation *ManagedInstanceOperation) BeforeUpdate(_ *gorm.DB) error {
	operation.UpdatedAt = common.GetTimestamp()
	return nil
}
