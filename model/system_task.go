package model

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/01121531/subandnew-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SystemTaskStatus string

const (
	SystemTaskStatusPending   SystemTaskStatus = "pending"
	SystemTaskStatusRunning   SystemTaskStatus = "running"
	SystemTaskStatusSucceeded SystemTaskStatus = "succeeded"
	SystemTaskStatusFailed    SystemTaskStatus = "failed"
	SystemTaskStatusCancelled SystemTaskStatus = "cancelled"

	SystemTaskTypeManagedInstanceProbe = "managed_instance_probe"
	SystemTaskTypeManagedInstanceSync  = "managed_instance_sync"
	SystemTaskTypeManagedUsageExport   = "managed_usage_export"
)

var ErrSystemTaskLockLost = errors.New("system task lock lost")

type SystemTask struct {
	ID        int64            `json:"id" gorm:"primary_key"`
	TaskID    string           `json:"task_id" gorm:"type:varchar(64);uniqueIndex"`
	Type      string           `json:"type" gorm:"type:varchar(64);index"`
	ScopeKey  string           `json:"scope_key" gorm:"type:varchar(128);not null;default:'';index"`
	Status    SystemTaskStatus `json:"status" gorm:"type:varchar(32);index"`
	ActiveKey *string          `json:"active_key,omitempty" gorm:"type:varchar(200);uniqueIndex"`
	Payload   string           `json:"payload" gorm:"type:text"`
	State     string           `json:"state" gorm:"type:text"`
	Result    string           `json:"result" gorm:"type:text"`
	Error     string           `json:"error" gorm:"type:text"`
	LockedBy  string           `json:"locked_by" gorm:"type:varchar(128);index"`
	CreatedAt int64            `json:"created_at" gorm:"bigint;index"`
	UpdatedAt int64            `json:"updated_at" gorm:"bigint;index"`
}

type SystemTaskLock struct {
	Type        string `json:"type" gorm:"type:varchar(64);primaryKey"`
	TaskID      string `json:"task_id" gorm:"type:varchar(64);index"`
	LockedBy    string `json:"locked_by" gorm:"type:varchar(128);index"`
	LockedUntil int64  `json:"locked_until" gorm:"bigint;index"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;index"`
}

type SystemTaskScopeLock struct {
	Type        string `json:"type" gorm:"type:varchar(64);primaryKey"`
	ScopeKey    string `json:"scope_key" gorm:"type:varchar(128);primaryKey"`
	TaskID      string `json:"task_id" gorm:"type:varchar(64);index"`
	LockedBy    string `json:"locked_by" gorm:"type:varchar(128);index"`
	LockedUntil int64  `json:"locked_until" gorm:"bigint;index"`
	UpdatedAt   int64  `json:"updated_at" gorm:"bigint;index"`
}

type SystemTaskResponse struct {
	ID        int64            `json:"id"`
	TaskID    string           `json:"task_id"`
	Type      string           `json:"type"`
	ScopeKey  string           `json:"scope_key"`
	Status    SystemTaskStatus `json:"status"`
	ActiveKey *string          `json:"active_key,omitempty"`
	Payload   any              `json:"payload"`
	State     any              `json:"state"`
	Result    any              `json:"result"`
	Error     string           `json:"error"`
	LockedBy  string           `json:"locked_by"`
	CreatedAt int64            `json:"created_at"`
	UpdatedAt int64            `json:"updated_at"`
}

func (task *SystemTask) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if task.CreatedAt == 0 {
		task.CreatedAt = now
	}
	if task.UpdatedAt == 0 {
		task.UpdatedAt = now
	}
	return nil
}

func (lock *SystemTaskLock) BeforeCreate(_ *gorm.DB) error {
	if lock.UpdatedAt == 0 {
		lock.UpdatedAt = common.GetTimestamp()
	}
	return nil
}

func (lock *SystemTaskScopeLock) BeforeCreate(_ *gorm.DB) error {
	if lock.UpdatedAt == 0 {
		lock.UpdatedAt = common.GetTimestamp()
	}
	return nil
}

func GenerateSystemTaskID() (string, error) {
	key, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", err
	}
	return "systask_" + key, nil
}

func CreateSystemTask(taskType string, payload any, state any) (*SystemTask, error) {
	return createSystemTask(taskType, payload, state, true)
}

// CreateQueuedSystemTask allows multiple pending rows of the same type. The
// per-type lease still guarantees that only one task is executed at a time.
func CreateQueuedSystemTask(taskType string, payload any, state any) (*SystemTask, error) {
	return createSystemTask(taskType, payload, state, false)
}

func CreateScopedSystemTask(taskType string, scopeKey string, payload any, state any) (*SystemTask, error) {
	scopeKey = normalizeSystemTaskScope(scopeKey)
	if scopeKey == "" {
		return nil, errors.New("system task scope key is required")
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
	activeKey := taskType + ":" + scopeKey
	task := &SystemTask{
		TaskID:    taskID,
		Type:      taskType,
		ScopeKey:  scopeKey,
		Status:    SystemTaskStatusPending,
		ActiveKey: &activeKey,
		Payload:   payloadText,
		State:     stateText,
	}
	if err := DB.Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

func createSystemTask(taskType string, payload any, state any, exclusive bool) (*SystemTask, error) {
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
		TaskID:  taskID,
		Type:    taskType,
		Status:  SystemTaskStatusPending,
		Payload: payloadText,
		State:   stateText,
	}
	if exclusive {
		task.ActiveKey = &taskType
	}

	if err := DB.Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

func ListCompletedSystemTasksByTypeBefore(taskType string, updatedBefore int64, limit int) ([]*SystemTask, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	var tasks []*SystemTask
	err := DB.Where("type = ? AND status = ? AND updated_at <= ?", taskType, SystemTaskStatusSucceeded, updatedBefore).
		Order("id asc").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func UpdateCompletedSystemTaskState(taskID string, state any) error {
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return err
	}
	return DB.Model(&SystemTask{}).
		Where("task_id = ? AND status IN ?", taskID, []string{
			string(SystemTaskStatusSucceeded),
			string(SystemTaskStatusFailed),
		}).
		Updates(map[string]any{
			"state":      stateText,
			"updated_at": common.GetTimestamp(),
		}).Error
}

func GetSystemTaskByTaskID(taskID string) (*SystemTask, error) {
	var task SystemTask
	if err := DB.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func GetActiveSystemTask(taskType string) (*SystemTask, error) {
	var task SystemTask
	err := DB.Where("type = ? AND status IN ?", taskType, activeSystemTaskStatuses()).
		Order("id desc").
		First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func GetActiveScopedSystemTask(taskType string, scopeKey string) (*SystemTask, error) {
	var task SystemTask
	err := DB.Where("type = ? AND scope_key = ? AND status IN ?", taskType, normalizeSystemTaskScope(scopeKey), activeSystemTaskStatuses()).
		Order("id desc").First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func FindPendingSystemTasks(taskType string, limit int) ([]*SystemTask, error) {
	var tasks []*SystemTask
	if limit <= 0 {
		limit = 1
	}
	err := DB.Where("type = ? AND status = ?", taskType, SystemTaskStatusPending).
		Order("id asc").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func FindEarliestPendingSystemTasks(taskTypes []string) (map[string]*SystemTask, error) {
	tasksByType := map[string]*SystemTask{}
	if len(taskTypes) == 0 {
		return tasksByType, nil
	}

	subQuery := DB.Model(&SystemTask{}).
		Select("MIN(id)").
		Where("type IN ? AND status = ?", taskTypes, SystemTaskStatusPending).
		Group("type")
	var tasks []*SystemTask
	if err := DB.Where("id IN (?)", subQuery).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, task := range tasks {
		tasksByType[task.Type] = task
	}
	return tasksByType, nil
}

func FindPendingSystemTasksByTypes(taskTypes []string, limit int) ([]*SystemTask, error) {
	if len(taskTypes) == 0 {
		return []*SystemTask{}, nil
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var tasks []*SystemTask
	err := DB.Where("type IN ? AND status = ?", taskTypes, SystemTaskStatusPending).
		Order("id asc").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

// RetireUnsupportedSystemTasks closes active rows whose handlers no longer
// exist after an upgrade. Historical rows remain available for audit.
func RetireUnsupportedSystemTasks(supportedTypes []string, now int64) (int64, error) {
	if len(supportedTypes) == 0 {
		return 0, errors.New("supported system task types are required")
	}
	var retired int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var tasks []SystemTask
		if err := tx.Select("task_id").
			Where("status IN ? AND type NOT IN ?", activeSystemTaskStatuses(), supportedTypes).
			Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}
		taskIDs := make([]string, 0, len(tasks))
		for _, task := range tasks {
			taskIDs = append(taskIDs, task.TaskID)
		}
		result := tx.Model(&SystemTask{}).
			Where("task_id IN ? AND status IN ?", taskIDs, activeSystemTaskStatuses()).
			Updates(map[string]any{
				"status":     SystemTaskStatusFailed,
				"active_key": nil,
				"locked_by":  "",
				"error":      "task_type_retired",
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		retired = result.RowsAffected
		if err := tx.Where("task_id IN ?", taskIDs).Delete(&SystemTaskLock{}).Error; err != nil {
			return err
		}
		return tx.Where("task_id IN ?", taskIDs).Delete(&SystemTaskScopeLock{}).Error
	})
	return retired, err
}

func ListSystemTasks(limit int) ([]*SystemTask, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var tasks []*SystemTask
	err := DB.Order("id desc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// GetLatestSystemTask returns the most recent task row of the given type
// (any status) so the scheduler can decide whether enough time has elapsed
// since the last run. Returns (nil, nil) when no row exists.
func GetLatestSystemTask(taskType string) (*SystemTask, error) {
	var task SystemTask
	err := DB.Where("type = ?", taskType).Order("id desc").First(&task).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func GetLatestSystemTasks(taskTypes []string) (map[string]*SystemTask, error) {
	tasksByType := map[string]*SystemTask{}
	if len(taskTypes) == 0 {
		return tasksByType, nil
	}

	subQuery := DB.Model(&SystemTask{}).
		Select("MAX(id)").
		Where("type IN ?", taskTypes).
		Group("type")
	var tasks []*SystemTask
	if err := DB.Where("id IN (?)", subQuery).Find(&tasks).Error; err != nil {
		return nil, err
	}
	for _, task := range tasks {
		tasksByType[task.Type] = task
	}
	return tasksByType, nil
}

func ClaimSystemTask(id int64, taskType string, runnerID string, lockUntil int64) (*SystemTask, bool, error) {
	now := common.GetTimestamp()
	var task SystemTask
	if err := DB.Where("id = ? AND type = ? AND status = ?", id, taskType, SystemTaskStatusPending).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}

	acquired, expiredTaskID, err := acquireSystemTaskLock(taskType, task.TaskID, runnerID, now, lockUntil)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	if expiredTaskID != "" && expiredTaskID != task.TaskID {
		if err := MarkSystemTaskLeaseExpired(expiredTaskID); err != nil {
			_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
			return nil, false, err
		}
	}

	result := DB.Model(&SystemTask{}).
		Where("id = ? AND type = ? AND status = ?", id, taskType, SystemTaskStatusPending).
		Updates(map[string]any{
			"status":     SystemTaskStatusRunning,
			"locked_by":  runnerID,
			"updated_at": now,
		})
	if result.Error != nil {
		_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
		return nil, false, nil
	}

	if err := DB.Where("id = ?", id).First(&task).Error; err != nil {
		return nil, false, err
	}
	return &task, true, nil
}

func ClaimScopedSystemTask(id int64, taskType string, runnerID string, lockUntil int64) (*SystemTask, bool, error) {
	now := common.GetTimestamp()
	var task SystemTask
	if err := DB.Where("id = ? AND type = ? AND scope_key <> '' AND status = ?", id, taskType, SystemTaskStatusPending).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	acquired, expiredTaskID, err := acquireScopedSystemTaskLock(task.Type, task.ScopeKey, task.TaskID, runnerID, now, lockUntil)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	if expiredTaskID != "" && expiredTaskID != task.TaskID {
		if err := MarkSystemTaskLeaseExpired(expiredTaskID); err != nil {
			_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
			return nil, false, err
		}
	}
	result := DB.Model(&SystemTask{}).
		Where("id = ? AND type = ? AND scope_key = ? AND status = ?", id, taskType, task.ScopeKey, SystemTaskStatusPending).
		Updates(map[string]any{"status": SystemTaskStatusRunning, "locked_by": runnerID, "updated_at": now})
	if result.Error != nil {
		_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		_ = ReleaseSystemTaskLock(task.TaskID, runnerID)
		return nil, false, nil
	}
	if err := DB.Where("id = ?", id).First(&task).Error; err != nil {
		return nil, false, err
	}
	return &task, true, nil
}

func acquireScopedSystemTaskLock(taskType string, scopeKey string, taskID string, lockedBy string, now int64, lockUntil int64) (bool, string, error) {
	lock := &SystemTaskScopeLock{Type: taskType, ScopeKey: scopeKey, TaskID: taskID, LockedBy: lockedBy, LockedUntil: lockUntil, UpdatedAt: now}
	if err := DB.Create(lock).Error; err == nil {
		return true, "", nil
	}
	var existing SystemTaskScopeLock
	err := DB.Where("type = ? AND scope_key = ?", taskType, scopeKey).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, "", nil
		}
		return false, "", err
	}
	if existing.LockedUntil >= now {
		return false, "", nil
	}
	result := DB.Model(&SystemTaskScopeLock{}).
		Where("type = ? AND scope_key = ? AND locked_until < ?", taskType, scopeKey, now).
		Updates(map[string]any{"task_id": taskID, "locked_by": lockedBy, "locked_until": lockUntil, "updated_at": now})
	if result.Error != nil {
		return false, "", result.Error
	}
	if result.RowsAffected == 0 {
		return false, "", nil
	}
	return true, existing.TaskID, nil
}

func acquireSystemTaskLock(taskType string, taskID string, lockedBy string, now int64, lockUntil int64) (bool, string, error) {
	lock := &SystemTaskLock{
		Type:        taskType,
		TaskID:      taskID,
		LockedBy:    lockedBy,
		LockedUntil: lockUntil,
		UpdatedAt:   now,
	}
	if err := DB.Create(lock).Error; err == nil {
		return true, "", nil
	}

	var existing SystemTaskLock
	err := DB.Where("type = ?", taskType).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, "", nil
		}
		return false, "", err
	}
	if existing.LockedUntil >= now {
		return false, "", nil
	}

	result := DB.Model(&SystemTaskLock{}).
		Where("type = ? AND locked_until < ?", taskType, now).
		Updates(map[string]any{
			"task_id":      taskID,
			"locked_by":    lockedBy,
			"locked_until": lockUntil,
			"updated_at":   now,
		})
	if result.Error != nil {
		return false, "", result.Error
	}
	if result.RowsAffected == 0 {
		return false, "", nil
	}
	return true, existing.TaskID, nil
}

func UpdateSystemTaskState(taskID string, lockedBy string, state any) error {
	stateText, err := marshalSystemTaskJSON(state)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	query, err := withValidSystemTaskLock(
		DB.Model(&SystemTask{}).
			Where("task_id = ? AND status = ? AND locked_by = ?", taskID, SystemTaskStatusRunning, lockedBy),
		taskID,
		lockedBy,
		now,
	)
	if err != nil {
		return err
	}
	result := query.
		Updates(map[string]any{
			"state":      stateText,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSystemTaskLockLost
	}
	return nil
}

func RenewSystemTaskLock(taskID string, lockedBy string, lockUntil int64) error {
	now := common.GetTimestamp()
	result := DB.Model(&SystemTaskLock{}).
		Where("task_id = ? AND locked_by = ? AND locked_until >= ?", taskID, lockedBy, now).
		Updates(map[string]any{
			"locked_until": lockUntil,
			"updated_at":   now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		result = DB.Model(&SystemTaskScopeLock{}).
			Where("task_id = ? AND locked_by = ? AND locked_until >= ?", taskID, lockedBy, now).
			Updates(map[string]any{"locked_until": lockUntil, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrSystemTaskLockLost
		}
	}
	return nil
}

// RequireValidSystemTaskLease locks and validates the task lease inside the
// caller's transaction so stale workers cannot commit business results.
func RequireValidSystemTaskLease(tx *gorm.DB, taskID string, lockedBy string, now int64) error {
	if tx == nil || taskID == "" || lockedBy == "" {
		return ErrSystemTaskLockLost
	}
	var task SystemTask
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("task_id", "scope_key", "status", "locked_by").
		Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSystemTaskLockLost
		}
		return err
	}
	if task.Status != SystemTaskStatusRunning || task.LockedBy != lockedBy {
		return ErrSystemTaskLockLost
	}
	if task.ScopeKey == "" {
		var lock SystemTaskLock
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_id = ? AND locked_by = ? AND locked_until >= ?", taskID, lockedBy, now).
			First(&lock).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSystemTaskLockLost
			}
			return err
		}
		return nil
	}
	var lock SystemTaskScopeLock
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("task_id = ? AND locked_by = ? AND locked_until >= ?", taskID, lockedBy, now).
		First(&lock).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSystemTaskLockLost
		}
		return err
	}
	return nil
}

func MarkSystemTaskLeaseExpired(taskID string) error {
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		var task SystemTask
		if err := tx.Where("task_id = ? AND status = ?", taskID, SystemTaskStatusRunning).First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if task.Type == SystemTaskTypeManagedUsageExport {
			return RequeueExpiredManagedUsageExportLease(tx, taskID, now)
		}
		taskUpdate := tx.Model(&SystemTask{}).Where("id = ? AND status = ?", task.ID, SystemTaskStatusRunning).
			Updates(map[string]any{
				"status": SystemTaskStatusFailed, "active_key": nil,
				"error": "task lease expired", "updated_at": now,
			})
		if taskUpdate.Error != nil {
			return taskUpdate.Error
		}
		if taskUpdate.RowsAffected == 0 {
			return nil
		}
		if task.Type != SystemTaskTypeManagedInstanceOperation {
			return nil
		}
		var operation ManagedInstanceOperation
		result := tx.Where("task_id = ? AND status IN ?", taskID, []string{
			ManagedInstanceOperationStatusQueued,
			ManagedInstanceOperationStatusRunning,
		}).First(&operation)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if result.Error != nil {
			return result.Error
		}
		operationStatus := ManagedInstanceOperationStatusFailed
		errorCode := "task_lease_expired"
		outcome := "failed"
		if operation.Status == ManagedInstanceOperationStatusRunning && operation.WritesRemote {
			operationStatus = ManagedInstanceOperationStatusUnknown
			errorCode = "remote_result_unknown"
			outcome = "unknown"
		}
		if err := tx.Model(&ManagedInstanceOperation{}).Where("id = ?", operation.Id).Updates(map[string]any{
			"status": operationStatus, "error_code": errorCode,
			"finished_at": now, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		details, _ := json.Marshal(map[string]any{
			"operation_id": operation.OperationId,
			"action":       operation.Action,
			"error_code":   errorCode,
		})
		actorID := operation.ExecutedBy
		if actorID == 0 {
			actorID = operation.ActorId
		}
		return tx.Create(&ManagedInstanceAudit{
			InstanceId: operation.InstanceId, ActorId: actorID, Action: "operation_complete",
			Outcome: outcome, Details: string(details), CreatedAt: now,
		}).Error
	})
}

func ExpireStaleSystemTaskLocks(now int64) error {
	var locks []*SystemTaskLock
	if err := DB.Where("locked_until < ?", now).Find(&locks).Error; err != nil {
		return err
	}
	for _, lock := range locks {
		if err := MarkSystemTaskLeaseExpired(lock.TaskID); err != nil {
			return err
		}
		result := DB.Where("type = ? AND task_id = ? AND locked_by = ? AND locked_until < ?", lock.Type, lock.TaskID, lock.LockedBy, now).
			Delete(&SystemTaskLock{})
		if result.Error != nil {
			return result.Error
		}
	}
	if DB.Migrator().HasTable(&SystemTaskScopeLock{}) {
		var scopedLocks []*SystemTaskScopeLock
		if err := DB.Where("locked_until < ?", now).Find(&scopedLocks).Error; err != nil {
			return err
		}
		for _, lock := range scopedLocks {
			if err := MarkSystemTaskLeaseExpired(lock.TaskID); err != nil {
				return err
			}
			result := DB.Where("type = ? AND scope_key = ? AND task_id = ? AND locked_by = ? AND locked_until < ?", lock.Type, lock.ScopeKey, lock.TaskID, lock.LockedBy, now).
				Delete(&SystemTaskScopeLock{})
			if result.Error != nil {
				return result.Error
			}
		}
	}
	return nil
}

func ReleaseSystemTaskLock(taskID string, lockedBy string) error {
	result := DB.Where("task_id = ? AND locked_by = ?", taskID, lockedBy).Delete(&SystemTaskLock{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	return DB.Where("task_id = ? AND locked_by = ?", taskID, lockedBy).Delete(&SystemTaskScopeLock{}).Error
}

func FinishSystemTask(taskID string, lockedBy string, status SystemTaskStatus, resultPayload any, errorMessage string) error {
	resultText, err := marshalSystemTaskJSON(resultPayload)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	query, err := withValidSystemTaskLock(
		DB.Model(&SystemTask{}).
			Where("task_id = ? AND status = ? AND locked_by = ?", taskID, SystemTaskStatusRunning, lockedBy),
		taskID,
		lockedBy,
		now,
	)
	if err != nil {
		return err
	}
	result := query.
		Updates(map[string]any{
			"status":     status,
			"active_key": nil,
			"result":     resultText,
			"error":      errorMessage,
			"updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSystemTaskLockLost
	}
	return ReleaseSystemTaskLock(taskID, lockedBy)
}

func (task *SystemTask) DecodePayload(v any) error {
	return decodeSystemTaskJSONString(task.Payload, v)
}

func (task *SystemTask) DecodeState(v any) error {
	return decodeSystemTaskJSONString(task.State, v)
}

func (task *SystemTask) ToResponse() SystemTaskResponse {
	return SystemTaskResponse{
		ID:        task.ID,
		TaskID:    task.TaskID,
		Type:      task.Type,
		ScopeKey:  task.ScopeKey,
		Status:    task.Status,
		ActiveKey: task.ActiveKey,
		Payload:   decodeSystemTaskJSONValue(task.Payload),
		State:     decodeSystemTaskJSONValue(task.State),
		Result:    decodeSystemTaskJSONValue(task.Result),
		Error:     task.Error,
		LockedBy:  task.LockedBy,
		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
	}
}

func normalizeSystemTaskScope(scopeKey string) string {
	scopeKey = strings.TrimSpace(scopeKey)
	if len(scopeKey) > 128 {
		return scopeKey[:128]
	}
	return scopeKey
}

func withValidSystemTaskLock(query *gorm.DB, taskID string, lockedBy string, now int64) (*gorm.DB, error) {
	var task SystemTask
	if err := DB.Select("scope_key").Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return nil, err
	}
	if task.ScopeKey == "" {
		return query.Where("EXISTS (SELECT 1 FROM system_task_locks WHERE system_task_locks.task_id = system_tasks.task_id AND system_task_locks.locked_by = ? AND system_task_locks.locked_until >= ?)", lockedBy, now), nil
	}
	return query.Where("EXISTS (SELECT 1 FROM system_task_scope_locks WHERE system_task_scope_locks.task_id = system_tasks.task_id AND system_task_scope_locks.locked_by = ? AND system_task_scope_locks.locked_until >= ?)", lockedBy, now), nil
}

func activeSystemTaskStatuses() []string {
	return []string{string(SystemTaskStatusPending), string(SystemTaskStatusRunning)}
}

func marshalSystemTaskJSON(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	data, err := common.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeSystemTaskJSONString(data string, v any) error {
	if data == "" {
		return nil
	}
	return common.UnmarshalJsonStr(data, v)
}

func decodeSystemTaskJSONValue(data string) any {
	if data == "" {
		return nil
	}
	var value any
	if err := common.UnmarshalJsonStr(data, &value); err != nil {
		return data
	}
	return value
}
