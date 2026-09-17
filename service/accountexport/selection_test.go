package accountexport

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service"
	"github.com/01121531/subandnew-api/service/authz"
	"github.com/01121531/subandnew-api/service/managedaccount"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSelectionTest(t *testing.T, count int) (*gorm.DB, model.User, model.ManagedInstance) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.AdminDataPolicy{}, &model.ManagedInstance{}, &model.ManagedInstanceCredential{}, &model.ManagedAccountSnapshot{}, &model.ManagedInstanceSnapshot{}, &model.SystemTask{}, &model.SystemTaskScopeLock{}))
	old := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = old; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	user := model.User{Username: "export-test", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	instance := model.ManagedInstance{Name: "local-only", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://never-connect.invalid", Status: model.ManagedInstanceStatusHealthy}
	require.NoError(t, db.Create(&instance).Error)
	items := make([]managedinstance.InventoryItem, count)
	for i := range items {
		items[i] = managedinstance.InventoryItem{ID: int64(i + 1), Name: fmt.Sprintf("account-%05d", i+1)}
	}
	payload, err := json.Marshal(managedinstance.InventoryPage{Items: items, Total: count, ResourceKind: "account"})
	require.NoError(t, err)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.ManagedAccountSnapshot{InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindInventory, RangeKey: "inventory", SchemaVersion: 2, ObservedAt: now, Payload: string(payload), LastAttemptAt: now, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded}).Error)
	return db, user, instance
}

func TestPrepareScheduledExportIgnoresPaginationAndPreservesSort(t *testing.T) {
	_, user, instance := setupSelectionTest(t, 35)
	config := Config{Scope: "dynamic", Query: managedaccount.Query{InstanceIDs: []int64{instance.Id}, Dataset: "inventory", Page: 2, PageSize: 10, SortBy: "name", SortOrder: "desc"}}
	prepared, err := PrepareRun(t.Context(), user.Id, config, time.Now())
	require.NoError(t, err)
	require.Len(t, prepared.Export.Items, 35)
	require.Equal(t, int64(35), prepared.Export.Items[0].ResourceID)
	require.Zero(t, prepared.MissingCount)
	require.NotEmpty(t, prepared.Sources)
	var count int64
	require.NoError(t, model.DB.Model(&model.SystemTask{}).Count(&count).Error)
	require.Zero(t, count, "preparation must not enqueue or collect accounts")
}

func TestPrepareFixedScheduledExportCountsMissingAccounts(t *testing.T) {
	_, user, instance := setupSelectionTest(t, 3)
	config := Config{Scope: "fixed", Query: managedaccount.Query{InstanceIDs: []int64{instance.Id}, Dataset: "inventory", Search: "does-not-match"}, Accounts: []service.ManagedAccountExportItemInput{{InstanceID: instance.Id, AccountID: "1"}, {InstanceID: instance.Id, AccountID: "missing"}}}
	prepared, err := PrepareRun(t.Context(), user.Id, config, time.Now())
	require.NoError(t, err)
	require.Len(t, prepared.Export.Items, 1)
	require.Equal(t, 1, prepared.MissingCount)
	config.Accounts = append(config.Accounts, config.Accounts[0])
	_, err = PrepareRun(t.Context(), user.Id, config, time.Now())
	require.ErrorIs(t, err, ErrInvalidSchedule)
}

func TestPrepareScheduledExportRejectsOverLimitAndDisabledOwner(t *testing.T) {
	db, user, instance := setupSelectionTest(t, 10001)
	config := Config{Scope: "dynamic", Query: managedaccount.Query{InstanceIDs: []int64{instance.Id}, Dataset: "inventory"}}
	_, err := PrepareRun(t.Context(), user.Id, config, time.Now())
	require.ErrorIs(t, err, ErrAccountLimit)
	require.NoError(t, db.Model(&user).Update("status", common.UserStatusDisabled).Error)
	_, err = PrepareRun(t.Context(), user.Id, config, time.Now())
	require.ErrorIs(t, err, authz.ErrDataForbidden)
}

func TestPrepareScheduledExportDistinguishesEmptyFromMissingSnapshot(t *testing.T) {
	db, user, instance := setupSelectionTest(t, 3)
	config := Config{Scope: "dynamic", Query: managedaccount.Query{InstanceIDs: []int64{instance.Id}, Dataset: "inventory", Search: "missing-account"}}
	_, err := PrepareRun(t.Context(), user.Id, config, time.Now())
	require.ErrorIs(t, err, ErrNoAccounts)
	require.NoError(t, db.Where("instance_id = ?", instance.Id).Delete(&model.ManagedAccountSnapshot{}).Error)
	_, err = PrepareRun(t.Context(), user.Id, config, time.Now())
	require.ErrorIs(t, err, ErrSnapshotUnavailable)
}
