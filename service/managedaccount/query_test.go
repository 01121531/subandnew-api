package managedaccount

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLargeQuickAndAdvancedFiltersDoNotTruncate(t *testing.T) {
	db, instance := setupQueryTest(t)
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{{ID: 1, Name: "target-account"}, {ID: 2, Name: "other-account"}})
	values := make([]string, 1000)
	for i := range values {
		values[i] = fmt.Sprintf("missing-%04d", i)
	}
	values = append(values, " TARGET-ACCOUNT ", "target-account")
	normalized, err := normalizeTerms(values)
	require.NoError(t, err)
	require.Len(t, normalized, 1001)
	for _, negative := range []bool{false, true} {
		query := Query{InstanceIDs: []int64{instance.Id}, PageSize: 10}
		expected := "target-account"
		if negative {
			query.ExcludeTerms = values
			expected = "other-account"
		} else {
			query.IncludeTerms = values
		}
		result, err := Execute(t.Context(), query)
		require.NoError(t, err)
		require.Equal(t, 1, result.Total)
		require.Equal(t, 1, result.Summary.Total)
		require.Equal(t, expected, result.Items[0].Name)
	}
	result, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Rules: []managedinstance.AccountFilterRule{{Field: "name", Operator: "not_starts_with", Values: values, ValueMode: "any"}}})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, "other-account", result.Items[0].Name)
	_, err = normalizeTerms([]string{strings.Repeat("a", 201)})
	require.Error(t, err)
}

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

func TestExecuteFiltersAndSortsVendorsWithoutExpandingQuickSearch(t *testing.T) {
	db, instance := setupQueryTest(t)
	available := true
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{
		{ID: 1, Name: "alpha", VendorID: "vendor-2", VendorName: "Zen Supply", VendorEmail: "zen@example.com", Enabled: &available},
		{ID: 2, Name: "beta", VendorID: "vendor-1", VendorName: "Acme Supply", VendorEmail: "owner@acme.test", Enabled: &available},
	})

	result, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		MatchMode: managedinstance.AccountFilterMatchAll,
		Rules: []managedinstance.AccountFilterRule{
			{Field: "vendor_name", Operator: "is", Values: []string{"Acme Supply"}, ValueMode: managedinstance.AccountFilterValueAny},
			{Field: "vendor_email", Operator: "ends_with", Values: []string{"@acme.test"}, ValueMode: managedinstance.AccountFilterValueAny},
		},
		Page: 1, PageSize: 50})
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, "Acme Supply", result.Items[0].VendorName)
	require.Equal(t, "owner@acme.test", result.Items[0].VendorEmail)

	result, err = Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		SortBy: "vendor_name", SortOrder: "asc", Page: 1, PageSize: 50})
	require.NoError(t, err)
	require.Equal(t, []string{"Acme Supply", "Zen Supply"}, []string{result.Items[0].VendorName, result.Items[1].VendorName})

	result, err = Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		IncludeTerms: []string{"Acme Supply"}, Page: 1, PageSize: 50})
	require.NoError(t, err)
	require.Zero(t, result.Total, "vendor data must not broaden quick include search")
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

func TestExecuteMatchesTextPrefixSuffixAndContainsRules(t *testing.T) {
	db, instance := setupQueryTest(t)
	available := true
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{
		{ID: 1, Name: "allen-main", Email: "allen@example.com", Enabled: &available},
		{ID: 2, Name: "main-allen", Email: "allen@other.test", Enabled: &available},
		{ID: 3, Name: "main-allen-copy", Email: "copy@example.com", Enabled: &available},
	})

	tests := []struct {
		operator string
		value    string
		total    int
	}{
		{operator: "starts_with", value: "allen", total: 1},
		{operator: "ends_with", value: "allen", total: 1},
		{operator: "not_starts_with", value: " ALLEN ", total: 2},
		{operator: "not_ends_with", value: " ALLEN ", total: 2},
		{operator: "contains", value: "allen", total: 3},
	}
	for _, test := range tests {
		result, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
			MatchMode: managedinstance.AccountFilterMatchAll, Rules: []managedinstance.AccountFilterRule{{
				Field: "name", Operator: test.operator, Values: []string{test.value}, ValueMode: managedinstance.AccountFilterValueAny,
			}}, Page: 1, PageSize: 50})
		require.NoError(t, err, test.operator)
		require.Equal(t, test.total, result.Total, test.operator)
	}
}

func TestExecuteReturnsFilterOptionsFromAllMatchingPages(t *testing.T) {
	db, instance := setupQueryTest(t)
	available := true
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{
		{ID: 1, Name: "first", Group: "group-a", Status: "active", Enabled: &available},
		{ID: 2, Name: "second", Group: "group-b", Status: "paused", Enabled: &available},
	})
	result, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		NarrowFields: []string{"group", "status", "available"}, Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.ElementsMatch(t, []string{"group-a", "group-b"}, result.FilterOptions["group"])
	require.ElementsMatch(t, []string{"active", "paused"}, result.FilterOptions["status"])
}

func TestNegativeAffixRuleValueModesAndEmptyFields(t *testing.T) {
	for _, operator := range []string{"not_starts_with", "not_ends_with"} {
		t.Run(operator, func(t *testing.T) {
			rule := managedinstance.AccountFilterRule{Field: "name", Operator: operator, Values: []string{" TEST- "}, ValueMode: managedinstance.AccountFilterValueAny}
			require.False(t, ruleMatches([]string{"test-"}, rule))
			require.True(t, ruleMatches([]string{"middle-test-copy"}, rule))
			require.True(t, ruleMatches(nil, rule))
			require.True(t, ruleMatches([]string{"  "}, rule))
			require.False(t, ruleMatches([]string{"safe", "test-"}, rule))
			require.Equal(t, operator == "not_ends_with", ruleMatches([]string{"test-account"}, rule))
			require.Equal(t, operator == "not_starts_with", ruleMatches([]string{"account-test-"}, rule))
			rule.Values = []string{"test-", "other"}
			require.False(t, ruleMatches([]string{"test-"}, rule))
			rule.ValueMode = managedinstance.AccountFilterValueAll
			require.True(t, ruleMatches([]string{"test-"}, rule))
			require.False(t, ruleMatches([]string{"test-", "other"}, rule))
			rule.Values = []string{"供应商"}
			require.False(t, ruleMatches([]string{"供应商"}, rule))
			require.True(t, ruleMatches([]string{"我的供应商副本"}, rule))
		})
	}
}

func TestNegativeAffixFixedAndNarrowFiltersSummarizeBeforePagination(t *testing.T) {
	db, instance := setupQueryTest(t)
	cost := 4.5
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{
		{ID: 1, Name: "test-one", Email: "one@safe.test", Cost: &cost, CostUnit: "USD"},
		{ID: 2, Name: "two", Email: "two@example.com", Cost: &cost, CostUnit: "USD"},
		{ID: 3, Name: "three", Email: "three@safe.test", Cost: &cost, CostUnit: "USD"},
		{ID: 4, Name: "four", Cost: &cost, CostUnit: "USD"},
	})
	query := Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		MatchMode:       managedinstance.AccountFilterMatchAll,
		Rules:           []managedinstance.AccountFilterRule{{Field: "name", Operator: "not_starts_with", Values: []string{"test-"}, ValueMode: "any"}},
		NarrowMatchMode: "all", NarrowFields: []string{"email"},
		NarrowRules: []managedinstance.AccountFilterRule{{Field: "email", Operator: "not_ends_with", Values: []string{"@example.com"}, ValueMode: "any"}},
		Page:        1, PageSize: 1, SortBy: "name", SortOrder: "asc"}
	first, err := Execute(t.Context(), query)
	require.NoError(t, err)
	require.Equal(t, 2, first.Total)
	require.Len(t, first.Items, 1)
	require.Equal(t, 9.0, first.Summary.Amounts["USD"])
	query.Page = 2
	second, err := Execute(t.Context(), query)
	require.NoError(t, err)
	require.Equal(t, first.Summary, second.Summary)
	require.NotEqual(t, first.Items[0].AccountID, second.Items[0].AccountID)
	query.Page, query.PageSize, query.AllowLargePage = 1, 10000, true
	export, err := Execute(t.Context(), query)
	require.NoError(t, err)
	require.Len(t, export.Items, 2)
	require.Equal(t, first.Summary, export.Summary)
}

func TestExecuteReturnsVendorOptionsBeforeNarrowFilters(t *testing.T) {
	db, instance := setupQueryTest(t)
	available := true
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{
		{ID: 1, Name: "first", VendorName: "Acme Supply", VendorEmail: "owner@acme.test", Enabled: &available},
		{ID: 2, Name: "second", VendorName: "Beta Supply", VendorEmail: "owner@beta.test", Enabled: &available},
		{ID: 3, Name: "third", VendorName: "acme supply", VendorEmail: "OWNER@ACME.TEST", Enabled: &available},
		{ID: 4, Name: "fourth", VendorName: "平台自有", Enabled: &available},
		{ID: 5, Name: "fifth", VendorName: "未知供应商", Enabled: &available},
	})
	result, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory,
		NarrowFields: []string{"vendor_name", "vendor_email"}, NarrowRules: []managedinstance.AccountFilterRule{{
			Field: "vendor_name", Operator: "is", Values: []string{"Acme Supply"}, ValueMode: managedinstance.AccountFilterValueAny,
		}}, Page: 1, PageSize: 1})
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
	require.ElementsMatch(t, []string{"Acme Supply", "Beta Supply", "平台自有", "未知供应商"}, result.FilterOptions["vendor_name"])
	require.ElementsMatch(t, []string{"owner@acme.test", "owner@beta.test"}, result.FilterOptions["vendor_email"])
}

func TestSanitizeSensitiveTextRemovesIPv6AndHighEntropyCredentials(t *testing.T) {
	value := SanitizeSensitiveText("node 2409:8a55:3c14:19a1:ea08:73f4:323b:3d10 key AKIAABCDEFGHIJKLMNOP token 8xJ2mP9qR4sT7vW1yZ3aB6cD0eF5gH8jK2mN9pQ")
	require.NotContains(t, value, "2409:8a55")
	require.NotContains(t, value, "AKIAABCDEFGHIJKLMNOP")
	require.NotContains(t, value, "8xJ2mP9q")
	require.Contains(t, value, "[已隐藏]")
}

func TestExecuteRedactsSensitiveInstanceAndSourceFields(t *testing.T) {
	db, instance := setupQueryTest(t)
	require.NoError(t, db.Model(&model.ManagedInstance{}).Where("id = ?", instance.Id).Update("name", "[2409:8a55:3c14:19a1:ea08:73f4:323b:3d10]").Error)
	available := true
	sourceID := "AKIAABCDEFGHIJKLMNOP"
	sourceName := "node 2409:8a55:3c14:19a1:ea08:73f4:323b:3d10"
	now := time.Now().Unix()
	payload, err := json.Marshal(managedinstance.InventoryPage{ResourceKind: "account", Sources: []managedinstance.InventorySource{{ID: sourceID, Name: sourceName}}, Items: []managedinstance.InventoryItem{{
		ID: 1, Name: "token 8xJ2mP9qR4sT7vW1yZ3aB6cD0eF5gH8jK2mN9pQ", SourceID: sourceID, Enabled: &available,
	}}, Total: 1})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ManagedAccountSnapshot{InstanceID: instance.Id, SnapshotKind: model.ManagedAccountSnapshotKindInventory,
		RangeKey: "inventory", Timezone: TimezoneShanghai, SchemaVersion: 2, ObservedAt: now, Payload: string(payload),
		LastAttemptAt: now, LastAttemptStatus: model.ManagedInstanceCollectionSucceeded}).Error)

	result, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory, Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	item := result.Items[0]
	for _, value := range []string{item.InstanceName, item.Name, item.SourceID, item.SourceName} {
		require.NotContains(t, value, "2409:8a55")
		require.NotContains(t, value, "AKIAABCDEFGHIJKLMNOP")
		require.NotContains(t, value, "8xJ2mP9q")
		require.Contains(t, value, "[已隐藏]")
	}
}

func TestExecuteThrottlesSnapshotAccessTimestampWrites(t *testing.T) {
	db, instance := setupQueryTest(t)
	saveInventory(t, db, instance.Id, []managedinstance.InventoryItem{{ID: 1, Name: "first"}})
	recent := time.Now().Unix() - 30
	require.NoError(t, db.Model(&model.ManagedAccountSnapshot{}).Where("instance_id = ?", instance.Id).Update("last_accessed_at", recent).Error)
	_, err := Execute(t.Context(), Query{InstanceIDs: []int64{instance.Id}, Dataset: DatasetInventory, Page: 1, PageSize: 10})
	require.NoError(t, err)
	var snapshot model.ManagedAccountSnapshot
	require.NoError(t, db.Where("instance_id = ?", instance.Id).First(&snapshot).Error)
	require.Equal(t, recent, snapshot.LastAccessedAt)
}
