package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	ManagedConfigModeDisabled = "disabled"
	ManagedConfigModeAudit    = "audit"
	ManagedConfigModeEnforce  = "enforce"

	ManagedConfigDriftUnknown = "unknown"
	ManagedConfigDriftInSync  = "in_sync"
	ManagedConfigDriftDrifted = "drifted"
	ManagedConfigDriftFailed  = "failed"
)

// ManagedConfigTemplate stores only values admitted by the versioned product
// schema. It never stores an unfiltered remote settings document.
type ManagedConfigTemplate struct {
	Id            int64  `json:"id" gorm:"primaryKey"`
	Name          string `json:"name" gorm:"type:varchar(128);not null;uniqueIndex"`
	Description   string `json:"description" gorm:"type:varchar(512)"`
	Kind          string `json:"kind" gorm:"type:varchar(32);not null;index"`
	SchemaVersion int    `json:"schema_version" gorm:"not null"`
	Values        string `json:"-" gorm:"type:text;not null"`
	CreatedBy     int    `json:"created_by" gorm:"not null"`
	UpdatedBy     int    `json:"updated_by" gorm:"not null"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (ManagedConfigTemplate) TableName() string { return "managed_config_templates" }

func (template *ManagedConfigTemplate) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if template.CreatedAt == 0 {
		template.CreatedAt = now
	}
	template.UpdatedAt = now
	return nil
}

func (template *ManagedConfigTemplate) BeforeUpdate(_ *gorm.DB) error {
	template.UpdatedAt = common.GetTimestamp()
	return nil
}

// ManagedInstanceConfigBinding links one instance to its desired template and
// records only the last normalized, whitelisted observation hashes.
type ManagedInstanceConfigBinding struct {
	Id               int64  `json:"id" gorm:"primaryKey"`
	InstanceId       int64  `json:"instance_id" gorm:"not null;uniqueIndex"`
	TemplateId       int64  `json:"template_id" gorm:"not null;index"`
	Mode             string `json:"mode" gorm:"type:varchar(16);not null;default:'audit';index"`
	DriftStatus      string `json:"drift_status" gorm:"type:varchar(16);not null;default:'unknown';index"`
	DesiredHash      string `json:"desired_hash" gorm:"type:char(64)"`
	LastObservedHash string `json:"last_observed_hash" gorm:"type:char(64)"`
	LastErrorCode    string `json:"last_error_code,omitempty" gorm:"type:varchar(64)"`
	LastCheckedAt    int64  `json:"last_checked_at" gorm:"bigint;not null;default:0"`
	LastAppliedAt    int64  `json:"last_applied_at" gorm:"bigint;not null;default:0"`
	CreatedBy        int    `json:"created_by" gorm:"not null"`
	UpdatedBy        int    `json:"updated_by" gorm:"not null"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint;not null"`
}

func (ManagedInstanceConfigBinding) TableName() string {
	return "managed_instance_config_bindings"
}

func (binding *ManagedInstanceConfigBinding) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if binding.Mode == "" {
		binding.Mode = ManagedConfigModeAudit
	}
	if binding.DriftStatus == "" {
		binding.DriftStatus = ManagedConfigDriftUnknown
	}
	if binding.CreatedAt == 0 {
		binding.CreatedAt = now
	}
	binding.UpdatedAt = now
	return nil
}

func (binding *ManagedInstanceConfigBinding) BeforeUpdate(_ *gorm.DB) error {
	binding.UpdatedAt = common.GetTimestamp()
	return nil
}
