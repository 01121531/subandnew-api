package service

import (
	"os"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	if err := db.AutoMigrate(
		&model.User{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
		&model.SystemTaskScopeLock{},
		&model.ManagedInstance{},
		&model.ManagedInstanceCredential{},
		&model.ManagedInstanceOperation{},
		&model.ManagedInstanceOperationBatch{},
		&model.ManagedInstanceOperationBatchItem{},
		&model.ManagedInstanceAudit{},
		&model.ManagedInstanceSnapshot{},
		&model.ManagedInstanceAlert{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}
	os.Exit(m.Run())
}

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, table := range []string{
			"users",
			"system_task_locks", "system_task_scope_locks", "system_tasks",
			"managed_instance_operation_batch_items", "managed_instance_operation_batches",
			"managed_instance_operations", "managed_instance_credentials",
			"managed_instance_audits", "managed_instance_snapshots",
			"managed_instance_alerts", "managed_instances",
		} {
			model.DB.Exec("DELETE FROM " + table)
		}
	})
}
