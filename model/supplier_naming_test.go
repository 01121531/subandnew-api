package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSupplierNamingMigrationCompatibility(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.NoError(t, db.AutoMigrate(&Supplier{}, &SupplierBinding{}, &SupplierAudit{}))
	owner := Supplier{Name: "Legacy", Username: "legacy", Enabled: true, ViewAccounts: true, PasswordHash: "fixture"}
	require.NoError(t, db.Create(&owner).Error)
	b := SupplierBinding{SupplierID: owner.ID, InstanceID: 1, RemoteUserID: "fixture", Enabled: true}
	require.NoError(t, db.Create(&b).Error)
	// Recreate the pre-upgrade schema without touching any legacy values.
	for _, entry := range []struct {
		model  any
		column string
	}{{&Supplier{}, "naming_rule"}, {&Supplier{}, "naming_version"}, {&SupplierBinding{}, "naming_override"}, {&SupplierBinding{}, "naming_version"}, {&SupplierAudit{}, "naming_changes"}} {
		require.NoError(t, db.Migrator().DropColumn(entry.model, entry.column))
	}
	for range 2 {
		require.NoError(t, db.AutoMigrate(&Supplier{}, &SupplierBinding{}, &SupplierAudit{}))
	}
	require.NoError(t, db.First(&owner, owner.ID).Error)
	require.NoError(t, db.First(&b, b.ID).Error)
	require.Nil(t, owner.NamingRule)
	require.Nil(t, b.NamingOverride)
	require.Zero(t, owner.NamingVersion)
	require.Zero(t, b.NamingVersion)
	require.True(t, owner.ViewAccounts)
	require.True(t, b.Enabled)
	require.Equal(t, SupplierNamingRule{}, ResolveSupplierNaming(owner, &b).SupplierNamingRule)
	owner.NamingRule = &SupplierNamingRule{Prefix: "V-", Suffix: "-S"}
	owner.NamingVersion = 1
	b.NamingOverride = &SupplierNamingRule{}
	b.NamingVersion = 2
	require.NoError(t, db.Save(&owner).Error)
	require.NoError(t, db.Save(&b).Error)
	require.NoError(t, db.AutoMigrate(&Supplier{}, &SupplierBinding{}, &SupplierAudit{}))
	require.NoError(t, db.First(&owner, owner.ID).Error)
	require.NoError(t, db.First(&b, b.ID).Error)
	require.Equal(t, "V-", owner.NamingRule.Prefix)
	require.NotNil(t, b.NamingOverride)
	require.Equal(t, "binding", ResolveSupplierNaming(owner, &b).Source)
	require.Empty(t, ResolveSupplierNaming(owner, &b).Suffix)
}
