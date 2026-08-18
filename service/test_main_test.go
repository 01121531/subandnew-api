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
		&model.ManagedDashboardSnapshot{},
		&model.ManagedAccountSnapshot{},
		&model.ManagedRPMHistory{},
		&model.ManagedInstanceAlert{},
		&model.ManagedUsageExport{},
		&model.BillingFilterTemplate{},
		&model.BillingFilterTemplateVersion{},
		&model.BillingAlertRule{},
		&model.BillingAlertRuleInstance{},
		&model.BillingAlertThreshold{},
		&model.BillingCycleSnapshot{},
		&model.BillingEvaluationSnapshot{},
		&model.BillingAlertEvent{},
		&model.BillingEmailDelivery{},
		&model.ExchangeRate{},
		&model.ExchangeRateSetting{},
		&model.SMTPSetting{},
		&model.BillingAudit{},
		&model.BillingAlertExport{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}
	os.Exit(m.Run())
}

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, table := range []string{
			"billing_email_deliveries", "billing_alert_events", "billing_evaluation_snapshots",
			"billing_cycle_snapshots", "billing_alert_thresholds", "billing_alert_rule_instances",
			"billing_alert_rules", "billing_filter_template_versions", "billing_filter_templates",
			"exchange_rate_settings", "exchange_rates", "smtp_settings", "billing_audits", "billing_alert_exports",
			"users",
			"system_task_locks", "system_task_scope_locks", "system_tasks",
			"managed_instance_operation_batch_items", "managed_instance_operation_batches",
			"managed_instance_operations", "managed_instance_credentials",
			"managed_instance_audits", "managed_instance_snapshots", "managed_dashboard_snapshots", "managed_account_snapshots", "managed_rpm_history",
			"managed_instance_alerts", "managed_instances",
			"managed_usage_exports",
		} {
			model.DB.Exec("DELETE FROM " + table)
		}
	})
}
