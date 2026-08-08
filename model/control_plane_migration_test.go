package model

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var expectedControlPlaneTables = []string{
	"authz_roles",
	"casbin_rule",
	"custom_oauth_providers",
	"managed_instance_alerts",
	"managed_instance_audits",
	"managed_instance_credentials",
	"managed_instance_operation_batch_items",
	"managed_instance_operation_batches",
	"managed_instance_operations",
	"managed_instance_snapshots",
	"managed_instances",
	"options",
	"passkey_credentials",
	"setups",
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
