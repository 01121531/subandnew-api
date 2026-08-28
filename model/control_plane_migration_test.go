package model

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var expectedControlPlaneTables = []string{
	"assistant_binding_codes",
	"assistant_channel_leases",
	"assistant_channel_secrets",
	"assistant_channels",
	"assistant_conversations",
	"assistant_identities",
	"assistant_identity_instance_scopes",
	"assistant_inbox",
	"assistant_messages",
	"assistant_model_profiles",
	"assistant_outbox",
	"assistant_runs",
	"assistant_settings",
	"assistant_tool_calls",
	"authz_roles",
	"billing_alert_events",
	"billing_alert_exports",
	"billing_alert_rule_instances",
	"billing_alert_rules",
	"billing_alert_thresholds",
	"billing_audits",
	"billing_cycle_snapshots",
	"billing_email_deliveries",
	"billing_evaluation_snapshots",
	"billing_filter_template_versions",
	"billing_filter_templates",
	"casbin_rule",
	"custom_oauth_providers",
	"exchange_rate_settings",
	"exchange_rates",
	"managed_account_filter_templates",
	"managed_account_snapshots",
	"managed_config_templates",
	"managed_dashboard_snapshots",
	"managed_export_items",
	"managed_instance_alerts",
	"managed_instance_audits",
	"managed_instance_config_bindings",
	"managed_instance_credentials",
	"managed_instance_operation_batch_items",
	"managed_instance_operation_batches",
	"managed_instance_operations",
	"managed_instance_snapshots",
	"managed_instances",
	"managed_rpm_history",
	"managed_usage_exports",
	"metric_alert_conditions",
	"metric_alert_rule_instances",
	"metric_alert_rules",
	"metric_alert_states",
	"options",
	"passkey_credentials",
	"setups",
	"smtp_settings",
	"system_instances",
	"system_task_locks",
	"system_task_scope_locks",
	"system_tasks",
	"two_fa_backup_codes",
	"two_fas",
	"user_oauth_bindings",
	"users",
}

var representativeLegacyTables = []string{
	"abilities",
	"channel_proxy_bindings",
	"channels",
	"logs",
	"midjourneys",
	"proxy_groups",
	"proxies",
	"quota_data",
	"redemptions",
	"subscription_orders",
	"subscription_plans",
	"tokens",
	"top_ups",
	"user_subscriptions",
}

func TestMigrateDBCreatesOnlyControlPlaneTablesOnFreshSQLite(t *testing.T) {
	db := useControlPlaneMigrationTestDB(t)

	require.NoError(t, migrateDB())
	require.Equal(t, expectedControlPlaneTables, sqliteUserTables(t, db))
	require.True(t, db.Migrator().HasIndex(&ManagedInstanceOperationBatch{}, "uidx_managed_instance_batch_idempotency"))
	require.True(t, db.Migrator().HasIndex(&ManagedInstanceOperationBatchItem{}, "uidx_managed_instance_batch_target"))
	require.True(t, db.Migrator().HasIndex(&AssistantChannel{}, "uidx_assistant_channel_account"))
	require.True(t, db.Migrator().HasIndex(&AssistantChannelSecret{}, "idx_assistant_channel_secrets_channel_id"))
	require.True(t, db.Migrator().HasIndex(&AssistantIdentity{}, "uidx_assistant_identity_external"))
	require.True(t, db.Migrator().HasIndex(&AssistantIdentityInstanceScope{}, "uidx_assistant_identity_instance"))
	require.True(t, db.Migrator().HasIndex(&AssistantInboundEvent{}, "uidx_assistant_inbound_external"))
	require.True(t, db.Migrator().HasIndex(&AssistantConversation{}, "uidx_assistant_conversation_peer"))
	require.True(t, db.Migrator().HasIndex(&AssistantMessage{}, "uidx_assistant_message_turn"))
	require.True(t, db.Migrator().HasIndex(&AssistantRun{}, "uidx_assistant_run_public_id"))
	require.True(t, db.Migrator().HasIndex(&AssistantToolCall{}, "uidx_assistant_tool_call_order"))
	require.True(t, db.Migrator().HasIndex(&AssistantOutbox{}, "uidx_assistant_outbox_reply_key"))
	for _, column := range []string{
		"accounts_available_last", "accounts_total_last", "account_sample_count",
		"concurrency_used_last", "concurrency_max_last", "concurrency_sample_count", "concurrency_used_samples", "concurrency_max_samples",
		"today_cost_last", "today_cost_sample_count", "active_sessions_last", "active_session_samples",
	} {
		require.Truef(t, db.Migrator().HasColumn(&ManagedRPMHistory{}, column), "managed_rpm_history.%s must exist", column)
	}
	for _, table := range representativeLegacyTables {
		require.Falsef(t, db.Migrator().HasTable(table), "legacy table %q must not be created", table)
	}
}

func TestMigrateDBPreservesExistingLegacyTablesAndRows(t *testing.T) {
	db := useControlPlaneMigrationTestDB(t)
	legacyTables := append([]string{"legacy_operator_archive"}, representativeLegacyTables...)

	for _, table := range legacyTables {
		require.NoError(t, db.Exec(fmt.Sprintf(
			`CREATE TABLE %q (id INTEGER PRIMARY KEY, marker TEXT NOT NULL)`, table,
		)).Error)
		require.NoError(t, db.Exec(fmt.Sprintf(
			`INSERT INTO %q (id, marker) VALUES (1, 'preserve-me')`, table,
		)).Error)
	}

	require.NoError(t, migrateDB())
	for _, table := range legacyTables {
		require.Truef(t, db.Migrator().HasTable(table), "existing table %q must be preserved", table)
		var marker string
		require.NoError(t, db.Raw(fmt.Sprintf(
			`SELECT marker FROM %q WHERE id = 1`, table,
		)).Scan(&marker).Error)
		require.Equalf(t, "preserve-me", marker, "existing row in %q must be preserved", table)
	}
}

func TestMigrateDBPreservesLegacyUserColumnsAndValues(t *testing.T) {
	db := useControlPlaneMigrationTestDB(t)
	require.NoError(t, migrateDB())
	require.NoError(t, db.Exec(`ALTER TABLE users ADD COLUMN access_token TEXT`).Error)
	require.NoError(t, db.Exec(`ALTER TABLE users ADD COLUMN quota INTEGER`).Error)
	require.NoError(t, db.Exec(`ALTER TABLE users ADD COLUMN user_group TEXT`).Error)
	require.NoError(t, db.Exec(`INSERT INTO users
		(id, username, password, display_name, access_token, quota, user_group)
		VALUES (1, 'legacy-admin', 'legacy-password', 'Legacy Admin', 'legacy-token', 123456, 'legacy-group')`).Error)

	require.NoError(t, migrateDB())
	for _, column := range []string{"access_token", "quota", "user_group"} {
		require.Truef(t, db.Migrator().HasColumn("users", column), "legacy users.%s must be preserved", column)
	}
	var row struct {
		AccessToken string `gorm:"column:access_token"`
		Quota       int    `gorm:"column:quota"`
		Group       string `gorm:"column:user_group"`
	}
	require.NoError(t, db.Raw(`SELECT access_token, quota, user_group FROM users WHERE id = 1`).Scan(&row).Error)
	require.Equal(t, "legacy-token", row.AccessToken)
	require.Equal(t, 123456, row.Quota)
	require.Equal(t, "legacy-group", row.Group)
}

func TestLegacyCommercialOptionsArePreservedButIgnored(t *testing.T) {
	db := useControlPlaneMigrationTestDB(t)
	require.NoError(t, migrateDB())
	require.NoError(t, db.Create(&Option{Key: "ModelRatio", Value: `{"gpt-4":30}`}).Error)

	InitOptionMap()
	common.OptionMapRWMutex.RLock()
	_, loaded := common.OptionMap["ModelRatio"]
	common.OptionMapRWMutex.RUnlock()
	require.False(t, loaded)

	options, err := AllOption()
	require.NoError(t, err)
	for _, option := range options {
		require.NotEqual(t, "ModelRatio", option.Key)
	}
	require.Error(t, UpdateOption("ModelRatio", "{}"))

	var stored Option
	require.NoError(t, db.First(&stored, "key = ?", "ModelRatio").Error)
	require.Equal(t, `{"gpt-4":30}`, stored.Value)
}

func TestCloseDBIgnoresUninitializedLogDatabase(t *testing.T) {
	require.NoError(t, closeDB(nil))
}

func useControlPlaneMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "migration.db")), &gorm.Config{})
	require.NoError(t, err)

	previousDB := DB
	previousMainType := common.MainDatabaseType()
	DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	initCol()

	t.Cleanup(func() {
		DB = previousDB
		common.SetMainDatabaseType(previousMainType)
		initCol()
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			require.NoError(t, sqlDB.Close())
		}
	})

	return db
}

func sqliteUserTables(t *testing.T, db *gorm.DB) []string {
	t.Helper()

	var rows []struct {
		Name string `gorm:"column:name"`
	}
	require.NoError(t, db.Raw(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`,
	).Scan(&rows).Error)

	tables := make([]string, 0, len(rows))
	for _, row := range rows {
		tables = append(tables, row.Name)
	}
	sort.Strings(tables)
	return tables
}
