package model

import (
	"errors"

	"gorm.io/gorm"
)

var ErrExportScheduleConflict = errors.New("export schedule version or state changed")

type ManagedAccountExportSchedule struct {
	ID          int64  `json:"id" gorm:"primaryKey"`
	OwnerID     int    `json:"owner_id" gorm:"not null;index"`
	Name        string `json:"name" gorm:"type:varchar(128);not null"`
	Config      string `json:"-" gorm:"type:text;not null"`
	Version     int64  `json:"version" gorm:"not null;default:1"`
	Enabled     bool   `json:"enabled" gorm:"not null;default:false;index"`
	DeletedAt   int64  `json:"-" gorm:"not null;default:0;index"`
	NextAt      int64  `json:"next_at" gorm:"not null;default:0;index"`
	ActiveRunID int64  `json:"active_run_id" gorm:"not null;default:0"`
	ErrorCode   string `json:"error_code" gorm:"type:varchar(128)"`
	CreatedAt   int64  `json:"created_at" gorm:"not null"`
	UpdatedAt   int64  `json:"updated_at" gorm:"not null"`
}

type ManagedAccountExportRun struct {
	ID         int64 `json:"id" gorm:"primaryKey"`
	ScheduleID int64 `json:"schedule_id" gorm:"not null;uniqueIndex:idx_account_export_occurrence,priority:1"`
	// Occurrence distinguishes manual invocations without moving the planned slot.
	Occurrence      string `json:"occurrence" gorm:"type:varchar(80);not null;uniqueIndex:idx_account_export_occurrence,priority:2"`
	ScheduledAt     int64  `json:"scheduled_at" gorm:"not null;index"`
	ScheduleVersion int64  `json:"schedule_version" gorm:"not null"`
	TaskID          string `json:"task_id" gorm:"type:varchar(64);index"`
	FrozenConfig    string `json:"-" gorm:"type:text;not null"`
	Status          string `json:"status" gorm:"type:varchar(32);not null;index"`
	LeaseOwner      string `json:"-" gorm:"type:varchar(64)"`
	LeaseUntil      int64  `json:"-" gorm:"not null;default:0;index"`
	MissingCount    int    `json:"missing_count" gorm:"not null;default:0"`
	ErrorCode       string `json:"error_code" gorm:"type:varchar(128)"`
	CreatedAt       int64  `json:"created_at" gorm:"not null"`
	FinishedAt      int64  `json:"finished_at" gorm:"not null;default:0"`
}

type ManagedAccountExportDelivery struct {
	ID            int64  `json:"id" gorm:"primaryKey"`
	RunID         int64  `json:"run_id" gorm:"not null;uniqueIndex:idx_account_export_recipient,priority:1"`
	Recipient     string `json:"recipient" gorm:"type:varchar(254);not null;uniqueIndex:idx_account_export_recipient,priority:2"`
	Status        string `json:"status" gorm:"type:varchar(32);not null;index"`
	Version       int64  `json:"version" gorm:"not null;default:1"`
	Attempts      int    `json:"attempts" gorm:"not null;default:0"`
	NextAttemptAt int64  `json:"next_attempt_at" gorm:"not null;default:0;index"`
	LeaseOwner    string `json:"-" gorm:"type:varchar(64)"`
	LeaseUntil    int64  `json:"-" gorm:"not null;default:0;index"`
	ErrorCode     string `json:"error_code" gorm:"type:varchar(128)"`
	CreatedAt     int64  `json:"created_at" gorm:"not null"`
	SentAt        int64  `json:"sent_at" gorm:"not null;default:0"`
}

// ClaimAccountExportOccurrence must run in the same transaction that freezes
// selections and creates the SystemTask. A failed freeze rolls back the slot.
func ClaimAccountExportOccurrence(tx *gorm.DB, plan ManagedAccountExportSchedule, occurrence string, scheduledAt, nextAt, now int64, manual bool, worker string) (*ManagedAccountExportRun, error) {
	if plan.ID <= 0 || plan.Version <= 0 || occurrence == "" || scheduledAt <= 0 || worker == "" || (!manual && nextAt <= scheduledAt) {
		return nil, ErrExportScheduleConflict
	}
	updates := map[string]any{"updated_at": now}
	if !manual {
		updates["next_at"] = nextAt
	}
	query := tx.Model(&ManagedAccountExportSchedule{}).Where("id = ? AND version = ? AND enabled = ? AND deleted_at = 0 AND active_run_id = ?", plan.ID, plan.Version, true, plan.ActiveRunID)
	if !manual {
		query = query.Where("next_at = ?", scheduledAt)
	}
	// Updating version fences two workers even on databases that count matched
	// rows for an otherwise unchanged timestamp update.
	updates["version"] = gorm.Expr("version + 1")
	result := query.Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrExportScheduleConflict
	}
	run := &ManagedAccountExportRun{
		ScheduleID: plan.ID, Occurrence: occurrence, ScheduledAt: scheduledAt, ScheduleVersion: plan.Version,
		FrozenConfig: plan.Config, Status: "preparing", LeaseOwner: worker, LeaseUntil: now + 120, CreatedAt: now,
	}
	if plan.ActiveRunID != 0 {
		run.Status, run.ErrorCode, run.FinishedAt = "skipped", "overlapping_run", now
		run.LeaseOwner, run.LeaseUntil = "", 0
	}
	if err := tx.Create(run).Error; err != nil {
		return nil, err
	}
	if plan.ActiveRunID == 0 {
		if err := tx.Model(&ManagedAccountExportSchedule{}).Where("id = ?", plan.ID).Update("active_run_id", run.ID).Error; err != nil {
			return nil, err
		}
	}
	return run, nil
}

// RecoverAccountExportDeliveries never replays an interrupted SMTP attempt.
func RecoverAccountExportDeliveries(tx *gorm.DB, now int64) error {
	return tx.Model(&ManagedAccountExportDelivery{}).
		Where("status = ? AND lease_until <= ?", "sending", now).
		Updates(map[string]any{"status": "uncertain", "error_code": "delivery_interrupted", "lease_owner": "", "lease_until": 0, "version": gorm.Expr("version + 1")}).Error
}
