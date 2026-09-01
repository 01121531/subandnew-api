package service

import (
	"errors"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/managedinstance"
	"github.com/stretchr/testify/require"
)

func TestNormalizeManagedDashboardPresetUsesShanghaiNaturalDays(t *testing.T) {
	dashboardRange, err := NormalizeManagedDashboardRange(7, 0, 0)
	require.NoError(t, err)
	location, err := time.LoadLocation(managedDashboardTimezone)
	require.NoError(t, err)
	start := time.Unix(dashboardRange.Start, 0).In(location)
	end := time.Unix(dashboardRange.End, 0).In(location)
	require.Equal(t, 0, start.Hour())
	require.Equal(t, 0, start.Minute())
	require.Equal(t, 23, end.Hour())
	require.Equal(t, 59, end.Minute())
	require.Equal(t, 59, end.Second())
	require.Equal(t, 6, int(end.Sub(start).Hours()/24))
}

func TestDeriveManagedDashboardSummaryUsesRequestedDates(t *testing.T) {
	requests, tokens, cost := 99.0, 999.0, 99.0
	source := &managedinstance.SummaryResult{
		Requests: managedinstance.MetricSample{Value: &requests, Unit: "request", CollectionStatus: model.ManagedInstanceCollectionSucceeded},
		Tokens:   managedinstance.MetricSample{Value: &tokens, Unit: "token", CollectionStatus: model.ManagedInstanceCollectionSucceeded},
		Cost:     managedinstance.MetricSample{Value: &cost, Unit: "usd", CollectionStatus: model.ManagedInstanceCollectionSucceeded},
		Trend: []managedinstance.UsageTrendPoint{
			{Date: "2026-08-16", Requests: 1, Tokens: 10, Cost: 0.1},
			{Date: "2026-08-17", Requests: 2, Tokens: 20, Cost: 0.2},
			{Date: "2026-08-18", Requests: 3, Tokens: 30, Cost: 0.3},
		},
	}
	location, _ := time.LoadLocation(managedDashboardTimezone)
	dashboardRange := ManagedDashboardRange{
		RangeKey: "custom-test", Timezone: managedDashboardTimezone,
		Start: time.Date(2026, 8, 17, 0, 0, 0, 0, location).Unix(),
		End:   time.Date(2026, 8, 18, 23, 59, 59, 0, location).Unix(),
	}
	result := deriveManagedDashboardSummary(source, dashboardRange)
	require.Len(t, result.Trend, 2)
	require.Equal(t, 5.0, *result.Requests.Value)
	require.Equal(t, 50.0, *result.Tokens.Value)
	require.InDelta(t, 0.5, *result.Cost.Value, 0.000000001)
}

func TestManagedDashboardFailureKeepsLastSuccessfulPayload(t *testing.T) {
	truncate(t)
	instance := model.ManagedInstance{Name: "dashboard-cache-test", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://dashboard-cache.test"}
	require.NoError(t, model.DB.Create(&instance).Error)
	dashboardRange, err := NormalizeManagedDashboardRange(1, 0, 0)
	require.NoError(t, err)
	cost := 12.3456789
	summary := &managedinstance.SummaryResult{Cost: managedinstance.MetricSample{Value: &cost, Unit: "usd", CollectionStatus: model.ManagedInstanceCollectionSucceeded}}
	require.NoError(t, saveManagedDashboardSuccess(instance.Id, dashboardRange, time.Now().Unix(), summary))
	require.NoError(t, saveManagedDashboardFailure(instance.Id, dashboardRange, errors.New("temporary upstream failure")))
	section, err := loadManagedDashboardSection(instance.Id, dashboardRange)
	require.NoError(t, err)
	require.NotNil(t, section.Observation)
	retained := section.Observation.Data.(*managedinstance.SummaryResult)
	require.Equal(t, cost, *retained.Cost.Value)
	require.Equal(t, model.ManagedInstanceCollectionFailed, section.LastAttemptStatus)
	require.Equal(t, "collection_failed", section.LastErrorCode)
}

func TestManagedDashboardPresetFreshUsesFiveMinuteAttemptWindow(t *testing.T) {
	require.Equal(t, 5*time.Minute, managedDashboardRefreshInterval)
	require.Equal(t, 10*time.Minute, managedDashboardStaleAfter)
	truncate(t)
	instance := model.ManagedInstance{Name: "dashboard-freshness", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://dashboard-freshness.test"}
	require.NoError(t, model.DB.Create(&instance).Error)
	now := time.Now().Unix()
	dashboardRange, err := NormalizeManagedDashboardRange(1, 0, 0)
	require.NoError(t, err)
	snapshot := model.ManagedDashboardSnapshot{
		InstanceID: instance.Id, RangeKey: dashboardRange.RangeKey, PresetDays: 1,
		WindowStart: dashboardRange.Start, WindowEnd: dashboardRange.End, Timezone: managedDashboardTimezone,
		Payload: "{}", ObservedAt: now - 600, LastAttemptAt: now - 60,
	}
	require.NoError(t, model.DB.Create(&snapshot).Error)
	require.True(t, managedDashboardPresetFresh(instance.Id, now))
	require.False(t, managedDashboardPresetFresh(instance.Id, now+int64(managedDashboardRefreshInterval/time.Second)))
}

func TestRecordManagedDashboardCostPersistsRealZero(t *testing.T) {
	truncate(t)
	instance := model.ManagedInstance{Name: "dashboard-cost", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://dashboard-cost.test"}
	require.NoError(t, model.DB.Create(&instance).Error)
	zero := 0.0
	summary := &managedinstance.SummaryResult{Cost: managedinstance.MetricSample{
		Value: &zero, Unit: "usd", CollectionStatus: model.ManagedInstanceCollectionSucceeded,
	}}
	recordManagedDashboardCost(t.Context(), instance.Id, time.Now().Unix(), summary)
	var history model.ManagedRPMHistory
	require.NoError(t, model.DB.Where("instance_id = ?", instance.Id).First(&history).Error)
	require.Equal(t, 1, history.TodayCostSampleCount)
	require.Zero(t, history.TodayCostLast)
}
