package model

import (
	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	ManagedInstanceAlertTypeAvailability = "availability"
	ManagedInstanceAlertTypeCredential   = "credential"

	ManagedInstanceAlertStatusOpen     = "open"
	ManagedInstanceAlertStatusResolved = "resolved"

	ManagedInstanceAlertEmailPending   = "pending"
	ManagedInstanceAlertEmailRetrying  = "retrying"
	ManagedInstanceAlertEmailSent      = "sent"
	ManagedInstanceAlertEmailCancelled = "cancelled"
)

type ManagedInstanceAlert struct {
	Id               int64  `json:"id" gorm:"primaryKey"`
	InstanceId       int64  `json:"instance_id" gorm:"not null;index"`
	AlertType        string `json:"alert_type" gorm:"type:varchar(32);not null;index"`
	Status           string `json:"status" gorm:"type:varchar(16);not null;index"`
	ErrorCode        string `json:"error_code" gorm:"type:varchar(64);not null"`
	Occurrences      int    `json:"occurrences" gorm:"not null;default:1"`
	FirstSeenAt      int64  `json:"first_seen_at" gorm:"bigint;not null;index"`
	LastSeenAt       int64  `json:"last_seen_at" gorm:"bigint;not null;index"`
	ResolvedAt       int64  `json:"resolved_at" gorm:"bigint;not null;default:0;index"`
	EmailStatus      string `json:"email_status" gorm:"type:varchar(16);not null;default:'pending';index"`
	EmailRecipients  string `json:"email_recipients" gorm:"type:text;not null;default:''"`
	EmailAttempts    int    `json:"email_attempts" gorm:"not null;default:0"`
	EmailError       string `json:"email_error" gorm:"type:text"`
	EmailSentAt      int64  `json:"email_sent_at" gorm:"bigint;not null;default:0"`
	EmailNextRetryAt int64  `json:"email_next_retry_at" gorm:"bigint;not null;default:0;index"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt        int64  `json:"updated_at" gorm:"bigint;not null;index"`
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
	if alert.EmailStatus == "" {
		alert.EmailStatus = ManagedInstanceAlertEmailPending
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
