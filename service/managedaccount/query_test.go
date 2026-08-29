package managedaccount

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupQueryTest(t *testing.T) (*gorm.DB, model.ManagedInstance) {
	t.Helper()
	previous := model.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ManagedInstance{}, &model.ManagedInstanceCredential{}, &model.ManagedAccountSnapshot{}, &model.ManagedInstanceSnapshot{}, &model.SystemTask{}, &model.SystemTaskScopeLock{}))
	model.DB = db
	t.Cleanup(func() { model.DB = previous })
	instance := model.ManagedInstance{Name: "gateway-a", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://example.invalid", Status: model.ManagedInstanceStatusHealthy}
	require.NoError(t, db.Create(&instance).Error)
	return db, instance
}

func saveInventory(t *testing.T, db *gorm.DB, instanceID int64, items []managedinstance.InventoryItem) {
	t.Helper()
	now := time.Now().Unix()
	payload, err := json.Marshal(managedinstance.InventoryPage{ResourceKind: "account", Items: items, Total: len(items)})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ManagedAccountSnapshot{InstanceID: instanceID, SnapshotKind: model.ManagedAccountSnapshotKindInventory,
		RangeKey: "inventory", Timezone: TimezoneShanghai, SchemaVersion: 2, ObservedAt: now, Payload: string(payload),
		LastAttemptAt: now, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded}).Error)
}

func TestExecuteMatchesQuickAndAdvancedFilters(t *testing.T) {
	db, instance := setupQueryTest(t)
	available := true
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{
		{ID: 1, IDText: "90071992547409931", Name: "alpha", Email: "alpha@example.com", Note: "password=secret-value", Enabled: &available, Status: "active", CreatedAt: 10},
		{ID: 2, Name: "beta", Email: "beta@blocked.test", Enabled: &available, Status: "active", CreatedAt: 20},
	})
	result, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		IncludeTerms: []string{"alpha,beta"}, ExcludeTerms: []string{"blocked.test"}, MatchMode: managedinstance.AccountFilterMatchAll,
		Rules: []managedinstance.AccountFilterRule{{Field: "status", Operator: "is", Values: []string{"active"}, ValueMode: managedinstance.AccountFilterValueAny}},
		Page:  1, PageSize: 50})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, "90071992547409931", result.Items[0].AccountID)
	require.Contains(t, result.Items[0].Note, "[已隐藏]")
	require.NotContains(t, result.Items[0].Note, "secret-value")
	require.False(t, result.NoData)

	result, err = Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		IncludeTerms: []string{"gateway-a"}, Page: 1, PageSize: 50})
	require.NoError(t, err)
	require.Zero(t, result.Total, "instance names must not make every account match a quick filter")
}

func TestExecuteReturnsPartialWithoutInventingRows(t *testing.T) {
	db, instance := setupQueryTest(t)
	missing := model.ManagedInstance{Name: "missing", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://missing.invalid"}
	require.NoError(t, db.Create(&missing).Error)
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{{ID: 1, Name: "only"}})
	result, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id, missing.Id}, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.True(t, result.Partial)
	require.True(t, result.Stale)
	require.Equal(t, 1, result.Total)
	require.Len(t, result.Sources, 2)
}
