package model

import (
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func supplierModelDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&Supplier{}, &SupplierBinding{}, &SupplierSession{}, &SupplierOAuthFlow{}, &SupplierAudit{}))
	return db
}

func TestSupplierModelUniqueIdentitySurvivesSoftDelete(t *testing.T) {
	db := supplierModelDB(t)
	s := Supplier{Name: "Vendor", Username: "vendor", PasswordHash: "test-hash", Enabled: true, ViewAccounts: true, ViewUsage: true}
	require.NoError(t, db.Create(&s).Error)
	require.Positive(t, s.ID)
	require.Positive(t, s.CreatedAt)
	duplicate := Supplier{Name: "Other", Username: "vendor", PasswordHash: "other-hash"}
	require.Error(t, db.Create(&duplicate).Error)
	require.NoError(t, db.Delete(&s).Error)
	var count int64
	require.NoError(t, db.Model(&Supplier{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Unscoped().Model(&Supplier{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	duplicate.ID = 0
	require.Error(t, db.Create(&duplicate).Error)
}

func TestSupplierModelBindingCompositeUniqueness(t *testing.T) {
	db := supplierModelDB(t)
	insert := func(supplierID, instanceID int64, remoteID string) error {
		return db.Create(&SupplierBinding{SupplierID: supplierID, InstanceID: instanceID, RemoteUserID: remoteID, Ciphertext: "encrypted", KeyVersion: "v1", Revision: "revision", Enabled: true}).Error
	}
	require.NoError(t, insert(1, 10, "vendor-1"))
	require.Error(t, insert(1, 10, "vendor-2"), "one supplier may only bind an instance once")
	require.Error(t, insert(2, 10, "vendor-1"), "one upstream identity cannot belong to two suppliers on one instance")
	require.NoError(t, insert(1, 11, "vendor-1"), "same remote ID on a different instance is independent")
	require.NoError(t, insert(2, 10, "vendor-2"), "distinct remote identities may share an instance")
	require.NoError(t, db.Where("supplier_id = ? AND instance_id = ?", 1, 10).Delete(&SupplierBinding{}).Error)
	require.NoError(t, insert(3, 10, "vendor-1"), "deleted bindings release remote identity uniqueness")
}

func TestSupplierModelSessionAndOAuthTokensUnique(t *testing.T) {
	db := supplierModelDB(t)
	require.NoError(t, db.Create(&SupplierSession{SupplierID: 1, TokenHash: "session-hash"}).Error)
	require.Error(t, db.Create(&SupplierSession{SupplierID: 2, TokenHash: "session-hash"}).Error)
	require.NoError(t, db.Create(&SupplierOAuthFlow{SupplierID: 1, SessionID: 1, BindingID: 1, TokenHash: "flow-hash"}).Error)
	require.Error(t, db.Create(&SupplierOAuthFlow{SupplierID: 2, SessionID: 2, BindingID: 2, TokenHash: "flow-hash"}).Error)
	require.NoError(t, db.Create(&SupplierOAuthFlow{SupplierID: 1, SessionID: 1, BindingID: 1, TokenHash: "session-hash"}).Error, "flow and session token stores are independent")
}

func TestSupplierModelJSONHidesSecrets(t *testing.T) {
	for _, tc := range []struct {
		name      string
		value     any
		forbidden []string
	}{
		{"supplier", Supplier{ID: 1, Name: "Vendor", PasswordHash: "private-password-hash", AuthVersion: 17}, []string{"password", "Password", "auth_version", "AuthVersion", "private-password-hash", "deleted_at"}},
		{"binding", SupplierBinding{ID: 1, RemoteUserID: "private-remote-id", Ciphertext: "private-ciphertext", KeyVersion: "private-key-version", Revision: "private-revision", CacheVersion: 17}, []string{"remote_user_id", "ciphertext", "key_version", "revision", "cache_version", "private-"}},
		{"session", SupplierSession{ID: 1, SupplierID: 2, TokenHash: "private-token-hash", AuthVersion: 17, ExpiresAt: 123, LastUsedAt: 456}, []string{"token", "Token", "supplier_id", "auth_version", "last_used_at", "private-"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := json.Marshal(tc.value)
			require.NoError(t, err)
			for _, forbidden := range tc.forbidden {
				require.NotContains(t, string(encoded), forbidden)
			}
		})
	}
	encoded, err := json.Marshal(SupplierSession{ExpiresAt: 123})
	require.NoError(t, err)
	require.JSONEq(t, `{"expires_at":123}`, string(encoded))
}
