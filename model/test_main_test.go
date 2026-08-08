package model

import (
	"os"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&User{},
		&Token{},
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&TopUp{},
		&SubscriptionPlan{},
		&SubscriptionOrder{},
		&UserSubscription{},
		&UserOAuthBinding{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
		&SystemTaskScopeLock{},
		&ManagedInstance{},
		&ManagedInstanceCredential{},
		&ManagedInstanceOperation{},
		&ManagedInstanceAudit{},
		&ManagedInstanceSnapshot{},
		&ManagedInstanceAlert{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, table := range []string{
			"passkey_credentials", "two_fa_backup_codes", "two_fas", "tokens",
			"user_oauth_bindings", "users", "quota_data",
			"top_ups", "subscription_orders", "subscription_plans",
			"user_subscriptions", "system_instances",
			"system_task_locks", "system_task_scope_locks", "system_tasks",
			"managed_instance_operations", "managed_instance_credentials",
			"managed_instance_audits", "managed_instance_snapshots",
			"managed_instance_alerts", "managed_instances",
		} {
			DB.Exec("DELETE FROM " + table)
		}
	})
}
