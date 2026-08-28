package model

import (
	"os"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	initCol()

	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	if err := db.AutoMigrate(
		&User{},
		&PasskeyCredential{},
		&TwoFA{},
		&TwoFABackupCode{},
		&UserOAuthBinding{},
		&SystemInstance{},
		&SystemTask{},
		&SystemTaskLock{},
		&SystemTaskScopeLock{},
		&ManagedInstance{},
		&ManagedInstanceCredential{},
		&ManagedInstanceOperation{},
		&ManagedInstanceOperationBatch{},
		&ManagedInstanceOperationBatchItem{},
		&ManagedInstanceAudit{},
		&ManagedInstanceSnapshot{},
		&ManagedRPMHistory{},
		&ManagedInstanceAlert{},
		&ManagedUsageExport{},
		&ManagedExportItem{},
		&ManagedAccountFilterTemplate{},
		&ManagedConfigTemplate{},
		&ManagedInstanceConfigBinding{},
		&AssistantModelProfile{},
		&AssistantSetting{},
		&AssistantBindingCode{},
		&AssistantChannel{},
		&AssistantChannelSecret{},
		&AssistantChannelLease{},
		&AssistantIdentity{},
		&AssistantIdentityInstanceScope{},
		&AssistantInboundEvent{},
		&AssistantConversation{},
		&AssistantMessage{},
		&AssistantRun{},
		&AssistantToolCall{},
		&AssistantOutbox{},
		&BillingFilterTemplate{},
		&BillingFilterTemplateVersion{},
		&MetricAlertRule{},
		&MetricAlertRuleInstance{},
		&MetricAlertCondition{},
		&MetricAlertState{},
		&BillingAlertRule{},
		&BillingAlertRuleInstance{},
		&BillingAlertThreshold{},
		&BillingCycleSnapshot{},
		&BillingEvaluationSnapshot{},
		&BillingAlertEvent{},
		&BillingEmailDelivery{},
		&ExchangeRate{},
		&ExchangeRateSetting{},
		&SMTPSetting{},
		&BillingAudit{},
		&BillingAlertExport{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncateTables(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, table := range []string{
			"assistant_tool_calls", "assistant_outbox", "assistant_runs", "assistant_messages", "assistant_inbox",
			"assistant_conversations", "assistant_identity_instance_scopes", "assistant_identities",
			"assistant_binding_codes", "assistant_channel_leases", "assistant_channel_secrets", "assistant_channels", "assistant_model_profiles",
			"billing_email_deliveries", "billing_alert_events", "billing_evaluation_snapshots",
			"billing_cycle_snapshots", "billing_alert_thresholds", "billing_alert_rule_instances",
			"billing_alert_rules", "billing_filter_template_versions", "billing_filter_templates",
			"exchange_rate_settings", "exchange_rates", "smtp_settings", "billing_audits", "billing_alert_exports",
			"passkey_credentials", "two_fa_backup_codes", "two_fas",
			"user_oauth_bindings", "users", "system_instances",
			"system_task_locks", "system_task_scope_locks", "system_tasks",
			"managed_instance_operations", "managed_instance_credentials",
			"managed_instance_operation_batch_items", "managed_instance_operation_batches",
			"managed_instance_audits", "managed_instance_snapshots", "managed_rpm_history",
			"managed_instance_alerts", "managed_instances",
			"managed_usage_exports",
			"managed_export_items",
			"managed_account_filter_templates",
			"managed_instance_config_bindings", "managed_config_templates",
		} {
			DB.Exec("DELETE FROM " + table)
		}
	})
}
