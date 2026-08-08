package model

import (
	"sync"
	"testing"

	"github.com/01121531/subandnew-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestConfigureConnectionPoolSerializesSQLiteConnections(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:sqlite-pool-test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	t.Setenv("SQLITE_MAX_OPEN_CONNS", "")
	t.Setenv("SQLITE_BUSY_TIMEOUT_MS", "30000")
	require.NoError(t, configureConnectionPool(sqlDB, common.DatabaseTypeSQLite))
	require.Equal(t, 1, sqlDB.Stats().MaxOpenConnections)

	var busyTimeout int
	require.NoError(t, sqlDB.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout))
	require.Equal(t, 30000, busyTimeout)

	require.NoError(t, db.Exec("CREATE TABLE pool_test_counters (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)").Error)
	require.NoError(t, db.Exec("INSERT INTO pool_test_counters (id, value) VALUES (1, 0)").Error)
	const writers = 12
	start := make(chan struct{})
	errors := make(chan error, writers)
	var wg sync.WaitGroup
	for index := 0; index < writers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errors <- db.Transaction(func(tx *gorm.DB) error {
				return tx.Exec("UPDATE pool_test_counters SET value = value + 1 WHERE id = 1").Error
			})
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	for writeErr := range errors {
		require.NoError(t, writeErr)
	}
	var value int
	require.NoError(t, db.Raw("SELECT value FROM pool_test_counters WHERE id = 1").Scan(&value).Error)
	require.Equal(t, writers, value)
}

func TestConfigureConnectionPoolHonorsSQLiteOverride(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:sqlite-pool-override-test?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	t.Setenv("SQLITE_MAX_OPEN_CONNS", "3")
	require.NoError(t, configureConnectionPool(sqlDB, common.DatabaseTypeSQLite))
	require.Equal(t, 3, sqlDB.Stats().MaxOpenConnections)
}
