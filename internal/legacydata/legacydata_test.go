package legacydata

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestInventoryArchiveVerifyAndExplicitPurge(t *testing.T) {
	db := legacyTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE setups (id INTEGER PRIMARY KEY, version TEXT, initialized_at INTEGER)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO setups VALUES (1, 'v1', 100)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, role INTEGER, username TEXT, access_token TEXT, quota INTEGER, user_group TEXT)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO users VALUES (1, 100, 'root', 'legacy-secret', 123, 'legacy')`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE channels (id INTEGER PRIMARY KEY, name TEXT)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO channels VALUES (1, 'old-channel'), (2, 'old-channel-2')`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY, content TEXT)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO logs VALUES (1, 'old-log')`).Error)

	inventory, err := BuildInventory(db)
	require.NoError(t, err)
	require.Len(t, inventory.LegacyTables, 2)
	require.Equal(t, "channels", inventory.LegacyTables[0].Name)
	require.EqualValues(t, 2, inventory.LegacyTables[0].RowCount)
	require.Len(t, inventory.LegacyTables[0].ContentHash, 64)
	require.Equal(t, "logs", inventory.LegacyTables[1].Name)
	require.EqualValues(t, 1, inventory.LegacyTables[1].RowCount)
	require.Len(t, inventory.LegacyTables[1].ContentHash, 64)
	require.Equal(t, legacyUserColumns, inventory.LegacyUserColumns)
	require.Len(t, inventory.LegacyUserDataHash, 64)
	require.Len(t, inventory.DatabaseFingerprint, 64)

	archivePath := filepath.Join(t.TempDir(), "legacy-archive.json")
	checksum, err := WriteArchive(db, inventory, archivePath)
	require.NoError(t, err)
	require.Len(t, checksum, 64)
	header, err := VerifyArchive(archivePath)
	require.NoError(t, err)
	require.True(t, archiveCoversInventory(header, inventory))
	archiveBytes, err := os.ReadFile(archivePath)
	require.NoError(t, err)
	require.Contains(t, string(archiveBytes), "legacy-secret")
	require.Contains(t, string(archiveBytes), "old-channel")

	require.NoError(t, Purge(db, inventory))
	require.False(t, db.Migrator().HasTable("channels"))
	require.False(t, db.Migrator().HasTable("logs"))
	require.True(t, db.Migrator().HasTable("users"))
	require.True(t, db.Migrator().HasTable("setups"))
	for _, column := range legacyUserColumns {
		require.False(t, db.Migrator().HasColumn("users", column))
	}
	var username string
	require.NoError(t, db.Raw(`SELECT username FROM users WHERE id = 1`).Scan(&username).Error)
	require.Equal(t, "root", username)
}

func TestArchiveChecksumAndCoverageBlockUnsafePurge(t *testing.T) {
	db := legacyTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE channels (id INTEGER PRIMARY KEY)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO channels VALUES (1)`).Error)
	inventory, err := BuildInventory(db)
	require.NoError(t, err)
	archivePath := filepath.Join(t.TempDir(), "legacy-archive.json")
	_, err = WriteArchive(db, inventory, archivePath)
	require.NoError(t, err)
	header, err := VerifyArchive(archivePath)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE channels SET id = 2 WHERE id = 1`).Error)
	changedContent, err := BuildInventory(db)
	require.NoError(t, err)
	require.False(t, archiveCoversInventory(header, changedContent))
	require.Error(t, Purge(db, inventory))
	require.True(t, db.Migrator().HasTable("channels"))

	require.NoError(t, db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY)`).Error)
	changed, err := BuildInventory(db)
	require.NoError(t, err)
	require.False(t, archiveCoversInventory(header, changed))

	file, err := os.OpenFile(archivePath, os.O_APPEND|os.O_WRONLY, 0600)
	require.NoError(t, err)
	_, err = file.WriteString("tampered")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	_, err = VerifyArchive(archivePath)
	require.Error(t, err)
}

func TestUnknownTableIsNeverClassifiedOrPurgedAsLegacy(t *testing.T) {
	db := legacyTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE plugin_jobs (id INTEGER PRIMARY KEY, payload TEXT)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO plugin_jobs VALUES (1, 'keep-me')`).Error)

	inventory, err := BuildInventory(db)
	require.NoError(t, err)
	require.Empty(t, inventory.LegacyTables)
	require.Len(t, inventory.UnknownTables, 1)
	require.Equal(t, "plugin_jobs", inventory.UnknownTables[0].Name)

	_, err = WriteArchive(db, inventory, filepath.Join(t.TempDir(), "archive.json"))
	require.ErrorContains(t, err, "unknown database tables")
	require.ErrorContains(t, Purge(db, inventory), "unknown database tables")
	require.True(t, db.Migrator().HasTable("plugin_jobs"))
}

func TestVerifyArchiveRejectsSemanticallyTamperedRowsWithMatchingFileChecksum(t *testing.T) {
	db := legacyTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE channels (id INTEGER PRIMARY KEY, name TEXT)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO channels VALUES (1, 'original')`).Error)
	inventory, err := BuildInventory(db)
	require.NoError(t, err)
	archivePath := filepath.Join(t.TempDir(), "archive.json")
	_, err = WriteArchive(db, inventory, archivePath)
	require.NoError(t, err)

	contents, err := os.ReadFile(archivePath)
	require.NoError(t, err)
	contents = []byte(strings.Replace(string(contents), "original", "tampered", 1))
	require.NoError(t, os.WriteFile(archivePath, contents, 0600))
	digest := sha256.Sum256(contents)
	require.NoError(t, os.WriteFile(archivePath+".sha256", []byte(hex.EncodeToString(digest[:])+"  archive.json\n"), 0600))

	_, err = VerifyArchive(archivePath)
	require.ErrorContains(t, err, "content hash mismatch")
}

func legacyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return db
}
