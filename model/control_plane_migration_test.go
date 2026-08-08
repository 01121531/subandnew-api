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

func TestMigrateLogDBDoesNotCreateLegacyLogTable(t *testing.T) {
	db := useControlPlaneMigrationTestDB(t)

	require.NoError(t, migrateLOGDB())
	require.Empty(t, sqliteUserTables(t, db))
	require.False(t, db.Migrator().HasTable("logs"))
}

func TestCloseDBIgnoresUninitializedLogDatabase(t *testing.T) {
	require.NoError(t, closeDB(nil))
}

func useControlPlaneMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "migration.db")), &gorm.Config{})
	require.NoError(t, err)

	previousDB := DB
	previousLogDB := LOG_DB
	previousMainType := common.MainDatabaseType()
	previousLogType := common.LogDatabaseType()
	DB = db
	LOG_DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	initCol()

	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.SetMainDatabaseType(previousMainType)
		common.SetLogDatabaseType(previousLogType)
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
