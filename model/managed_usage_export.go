package model

import (
	"errors"

	"github.com/01121531/subandnew-api/common"
	"gorm.io/gorm"
)

const (
	ManagedUsageExportStatusPending   = "pending"
	ManagedUsageExportStatusRunning   = "running"
	ManagedUsageExportStatusSucceeded = "succeeded"
	ManagedUsageExportStatusFailed    = "failed"
	ManagedUsageExportStatusCancelled = "cancelled"
	ManagedUsageExportStatusExpired   = "expired"
)

var ErrManagedUsageExportConflict = errors.New("managed usage export status conflict")

type ManagedUsageExport struct {
	ID           int64  `json:"id" gorm:"primaryKey"`
	TaskID       string `json:"task_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	InstanceID   int64  `json:"instance_id" gorm:"not null;index"`
	InstanceName string `json:"instance_name" gorm:"type:varchar(128);not null"`
	InstanceKind string `json:"instance_kind" gorm:"type:varchar(32);not null;index"`
	ActorID      int    `json:"actor_id" gorm:"not null;index"`
	ActorName    string `json:"actor_name" gorm:"type:varchar(128);not null"`
	Query        string `json:"-" gorm:"type:text;not null"`
	Status       string `json:"status" gorm:"type:varchar(32);not null;index"`
	Progress     int    `json:"progress" gorm:"not null;default:0"`
	Processed    int64  `json:"processed" gorm:"bigint;not null;default:0"`
	Total        int64  `json:"total" gorm:"bigint;not null;default:0"`
	FileName     string `json:"file_name" gorm:"type:varchar(255)"`
	FileSize     int64  `json:"file_size" gorm:"bigint;not null;default:0"`
	RecordCount  int    `json:"record_count" gorm:"not null;default:0"`
	ErrorCode    string `json:"error_code" gorm:"type:varchar(128)"`
	StartedAt    int64  `json:"started_at" gorm:"bigint;not null;default:0"`
	FinishedAt   int64  `json:"finished_at" gorm:"bigint;not null;default:0"`
	ExpiresAt    int64  `json:"expires_at" gorm:"bigint;not null;default:0;index"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;not null;index"`
	UpdatedAt    int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

func (ManagedUsageExport) TableName() string { return "managed_usage_exports" }

func (export *ManagedUsageExport) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if export.Status == "" {
		export.Status = ManagedUsageExportStatusPending
	}
	if export.CreatedAt == 0 {
		export.CreatedAt = now
	}
	export.UpdatedAt = now
	return nil
}

type ManagedUsageExportListFilter struct {
	Status     string
	InstanceID int64
	ActorID    int
	Page       int
	PageSize   int
}

type ManagedUsageExportList struct {
	Items     []*ManagedUsageExport `json:"items"`
	Total     int64                 `json:"total"`
	Page      int                   `json:"page"`
	PageSize  int                   `json:"page_size"`
	HasActive bool                  `json:"has_active"`
}

func CreateManagedUsageExport(record *ManagedUsageExport, payload any, state any) (*SystemTask, error) {
	if record == nil || record.InstanceID <= 0 || record.ActorID <= 0 {
		return nil, errors.New("invalid managed usage export")
	}
	taskID, err := GenerateSystemTaskID()
	if err != nil {
		return nil, err
	}
	payloadText, err := marshalSystemTaskJSON(payload)
	if err != nil {
		return nil, err
	}
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return nil, err
	}
	task := &SystemTask{
		TaskID: taskID, Type: SystemTaskTypeManagedUsageExport,
		Status: SystemTaskStatusPending, Payload: payloadText, State: stateText,
	}
	record.TaskID = taskID
	record.Status = ManagedUsageExportStatusPending
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		return tx.Create(record).Error
	})
	return task, err
}

func ListManagedUsageExports(filter ManagedUsageExportListFilter) (*ManagedUsageExportList, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	query := DB.Model(&ManagedUsageExport{})
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.InstanceID > 0 {
		query = query.Where("instance_id = ?", filter.InstanceID)
	}
	if filter.ActorID > 0 {
		query = query.Where("actor_id = ?", filter.ActorID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	active := []string{ManagedUsageExportStatusPending, ManagedUsageExportStatusRunning}
	var activeCount int64
	if err := query.Where("status IN ?", active).Count(&activeCount).Error; err != nil {
		return nil, err
	}
	var items []*ManagedUsageExport
	err := query.
		Order("CASE WHEN status IN ('pending', 'running') THEN 0 ELSE 1 END ASC").
		Order("CASE WHEN status IN ('pending', 'running') THEN id END ASC").
		Order("id DESC").
		Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&items).Error
	if err != nil {
		return nil, err
	}
	return &ManagedUsageExportList{
		Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize, HasActive: activeCount > 0,
	}, nil
}

func GetManagedUsageExport(taskID string) (*ManagedUsageExport, error) {
	var record ManagedUsageExport
	if err := DB.Where("task_id = ?", taskID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func ManagedUsageExportQueuePosition(record *ManagedUsageExport) int64 {
	if record == nil {
		return 0
	}
	if record.Status == ManagedUsageExportStatusRunning {
		return 1
	}
	if record.Status != ManagedUsageExportStatusPending {
		return 0
	}
	var position int64
	DB.Model(&ManagedUsageExport{}).Where(
		"status = ? OR (status = ? AND id <= ?)",
		ManagedUsageExportStatusRunning, ManagedUsageExportStatusPending, record.ID,
	).Count(&position)
	return position
}

func StartManagedUsageExport(taskID string) error {
	now := common.GetTimestamp()
	result := DB.Model(&ManagedUsageExport{}).
		Where("task_id = ? AND status = ?", taskID, ManagedUsageExportStatusPending).
		Updates(map[string]any{
			"status": ManagedUsageExportStatusRunning, "progress": 0,
			"processed": 0, "total": 0, "error_code": "",
			"started_at": now, "finished_at": 0, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrManagedUsageExportConflict
	}
	return nil
}

func UpdateManagedUsageExportProgress(taskID string, progress int, processed int64, total int64) error {
	return DB.Model(&ManagedUsageExport{}).
		Where("task_id = ? AND status = ?", taskID, ManagedUsageExportStatusRunning).
		Updates(map[string]any{
			"progress": progress, "processed": processed, "total": total,
			"updated_at": common.GetTimestamp(),
		}).Error
}

func FinishManagedUsageExport(taskID string, status string, fileName string, fileSize int64, recordCount int, errorCode string, expiresAt int64) error {
	now := common.GetTimestamp()
	return DB.Model(&ManagedUsageExport{}).
		Where("task_id = ? AND status = ?", taskID, ManagedUsageExportStatusRunning).
		Updates(map[string]any{
			"status": status, "progress": map[bool]int{true: 100, false: 0}[status == ManagedUsageExportStatusSucceeded],
			"file_name": fileName, "file_size": fileSize, "record_count": recordCount,
			"error_code": errorCode, "finished_at": now, "expires_at": expiresAt, "updated_at": now,
		}).Error
}

func CancelManagedUsageExport(taskID string, actorID int, root bool) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&ManagedUsageExport{}).Where("task_id = ? AND status = ?", taskID, ManagedUsageExportStatusPending)
		if !root {
			query = query.Where("actor_id = ?", actorID)
		}
		now := common.GetTimestamp()
		result := query.Updates(map[string]any{"status": ManagedUsageExportStatusCancelled, "finished_at": now, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrManagedUsageExportConflict
		}
		result = tx.Model(&SystemTask{}).Where("task_id = ? AND status = ?", taskID, SystemTaskStatusPending).
			Updates(map[string]any{"status": SystemTaskStatusCancelled, "active_key": nil, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrManagedUsageExportConflict
		}
		return nil
	})
}

func RequeueManagedUsageExport(taskID string, lockedBy string) error {
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := RequireValidSystemTaskLease(tx, taskID, lockedBy, now); err != nil {
			return err
		}
		if err := tx.Model(&SystemTask{}).Where("task_id = ? AND status = ?", taskID, SystemTaskStatusRunning).
			Updates(map[string]any{"status": SystemTaskStatusPending, "locked_by": "", "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&ManagedUsageExport{}).Where("task_id = ?", taskID).
			Updates(map[string]any{
				"status": ManagedUsageExportStatusPending, "progress": 0, "processed": 0,
				"total": 0, "started_at": 0, "finished_at": 0, "error_code": "", "updated_at": now,
			}).Error; err != nil {
			return err
		}
		return tx.Where("task_id = ? AND locked_by = ?", taskID, lockedBy).Delete(&SystemTaskLock{}).Error
	})
}

func RequeueExpiredManagedUsageExportLease(tx *gorm.DB, taskID string, now int64) error {
	if err := tx.Exec(
		"UPDATE system_tasks SET status = ?, locked_by = ?, active_key = NULL, updated_at = ? WHERE task_id = ? AND status = ?",
		SystemTaskStatusPending, "", now, taskID, SystemTaskStatusRunning,
	).Error; err != nil {
		return err
	}
	return tx.Exec(
		"UPDATE managed_usage_exports SET status = ?, progress = 0, processed = 0, total = 0, started_at = 0, finished_at = 0, error_code = ?, updated_at = ? WHERE task_id = ?",
		ManagedUsageExportStatusPending, "", now, taskID,
	).Error
}

func ExpireManagedUsageExports(now int64) ([]string, error) {
	var records []*ManagedUsageExport
	if err := DB.Where("status = ? AND expires_at > 0 AND expires_at <= ?", ManagedUsageExportStatusSucceeded, now).Find(&records).Error; err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	taskIDs := make([]string, 0, len(records))
	for _, record := range records {
		taskIDs = append(taskIDs, record.TaskID)
	}
	err := DB.Model(&ManagedUsageExport{}).Where("task_id IN ?", taskIDs).
		Updates(map[string]any{"status": ManagedUsageExportStatusExpired, "updated_at": now}).Error
	return taskIDs, err
}

func ExpireManagedUsageExport(taskID string) error {
	return DB.Model(&ManagedUsageExport{}).
		Where("task_id = ? AND status = ?", taskID, ManagedUsageExportStatusSucceeded).
		Updates(map[string]any{"status": ManagedUsageExportStatusExpired, "updated_at": common.GetTimestamp()}).Error
}
