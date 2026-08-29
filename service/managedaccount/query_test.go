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
		{ID: 1, IDText: "90071992547409931", Name: "alpha", Email: "alpha@gmail.com", Note: "password=secret-value", Enabled: &available, Status: "active", CreatedAt: 10},
		{ID: 2, Name: "beta", Email: "beta@blocked.test", Enabled: &available, Status: "active", CreatedAt: 20},
	})
	result, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		IncludeTerms: []string{"alpha,beta"}, ExcludeTerms: []string{"beta@blocked.test"}, MatchMode: managedinstance.AccountFilterMatchAll,
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

	result, err = Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		IncludeTerms: []string{"ma"}, Page: 1, PageSize: 50})
	require.NoError(t, err)
	require.Zero(t, result.Total, "short terms must not match every gmail.com domain")
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

func TestExecuteMatchesMetricAndChinaTimeFilters(t *testing.T) {
	db, instance := setupQueryTest(t)
	available := true
	requestsHigh, requestsLow := 125.0, 12.0
	utilizationHigh, utilizationLow := 0.82, 0.25
	shanghai, err := time.LoadLocation(TimezoneShanghai)
	require.NoError(t, err)
	firstCreated := time.Date(2026, 8, 29, 9, 30, 0, 0, shanghai).Unix()
	secondCreated := time.Date(2026, 8, 28, 9, 30, 0, 0, shanghai).Unix()
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{
		{ID: 1, Name: "high", Enabled: &available, Requests: &requestsHigh, Utilization5H: &utilizationHigh, CreatedAt: firstCreated},
		{ID: 2, Name: "low", Enabled: &available, Requests: &requestsLow, Utilization5H: &utilizationLow, CreatedAt: secondCreated},
	})

	result, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		MatchMode: managedinstance.AccountFilterMatchAll,
		Rules: []managedinstance.AccountFilterRule{
			{Field: "requests", Operator: "gte", Values: []string{"100"}, ValueMode: managedinstance.AccountFilterValueAny},
			{Field: "utilization_5h", Operator: "between", Values: []string{"80", "90"}, ValueMode: managedinstance.AccountFilterValueAny},
			{Field: "created_at", Operator: "gte", Values: []string{"2026-08-29 00:00"}, ValueMode: managedinstance.AccountFilterValueAny},
		}, Page: 1, PageSize: 50})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, "high", result.Items[0].Name)
}

func TestExecuteSummarizesFilteredRowsBeforePagination(t *testing.T) {
	db, instance := setupQueryTest(t)
	available := true
	alphaCost, betaCost, gammaCost := 7.25, 5.25, 20.0
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{
		{ID: 1, Name: "alpha", Enabled: &available, Cost: &alphaCost, CostUnit: "USD"},
		{ID: 2, Name: "beta", Enabled: &available, Cost: &betaCost, CostUnit: "USD"},
		{ID: 3, Name: "gamma", Enabled: &available, Cost: &gammaCost, CostUnit: "quota"},
	})

	firstPage, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, 3, firstPage.Total)
	require.Equal(t, map[string]float64{"USD": 12.5, "quota": 20}, firstPage.Summary.Amounts)

	secondPage, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		Page: 2, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, firstPage.Summary.Amounts, secondPage.Summary.Amounts)

	filtered, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		NarrowIncludeTerms: []string{"alpha"}, Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, 1, filtered.Total)
	require.Equal(t, map[string]float64{"USD": 7.25}, filtered.Summary.Amounts)
}
