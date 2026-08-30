package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

type ManagedInstanceAlertRule struct {
	ID                   int64  `json:"id" gorm:"primaryKey"`
	Name                 string `json:"name" gorm:"type:varchar(128);not null"`
	Description          string `json:"description" gorm:"type:text"`
	Enabled              bool   `json:"enabled" gorm:"not null;default:true;index"`
	AlertTypes           string `json:"-" gorm:"type:text;not null"`
	CheckIntervalSeconds int    `json:"check_interval_seconds" gorm:"not null;default:60"`
	FailureThreshold     int    `json:"failure_threshold" gorm:"not null;default:0"`
	Recipients           string `json:"-" gorm:"type:text;not null;default:''"`
	CreatedBy            int    `json:"created_by" gorm:"not null;index"`
	UpdatedBy            int    `json:"updated_by" gorm:"not null"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedInstanceAlertRule) TableName() string { return "managed_instance_alert_rules" }

func (rule *ManagedInstanceAlertRule) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if rule.CreatedAt == 0 {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	return nil
}

func (rule *ManagedInstanceAlertRule) BeforeUpdate(_ *gorm.DB) error {
	rule.UpdatedAt = common.GetTimestamp()
	return nil
}

type ManagedInstanceAlertRuleInstance struct {
	ID         int64 `json:"id" gorm:"primaryKey"`
	RuleID     int64 `json:"rule_id" gorm:"not null;uniqueIndex:uidx_instance_alert_rule_scope,priority:1;index"`
	InstanceID int64 `json:"instance_id" gorm:"not null;uniqueIndex:uidx_instance_alert_rule_scope,priority:2;index"`
}

func (ManagedInstanceAlertRuleInstance) TableName() string {
	return "managed_instance_alert_rule_instances"
}

type ManagedInstanceAlertAssignment struct {
	InstanceID int64 `json:"instance_id" gorm:"primaryKey"`
	RuleID     int64 `json:"rule_id" gorm:"not null;index"`
	UpdatedAt  int64 `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedInstanceAlertAssignment) TableName() string {
	return "managed_instance_alert_assignments"
}
