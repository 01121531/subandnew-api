package managedinstance

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestConductorRPMHistoryAggregatesInstancesByMinuteAndHour(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instances := []model.ManagedInstance{
		{Name: "conductor-one", Kind: model.ManagedInstanceKindConductor, BaseURL: "https://one.example.com"},
		{Name: "conductor-two", Kind: model.ManagedInstanceKindConductor, BaseURL: "https://two.example.com"},
	}
	if err := db.Create(&instances).Error; err != nil {
		t.Fatalf("create instances: %v", err)
	}
	start := int64(1_786_593_600)
	ctx := context.Background()
	for _, sample := range []struct {
		instanceID  int64
		at          int64
		rpm         float64
		capacity    float64
		hasCapacity bool
	}{
		{instances[0].Id, start + 10, 10, 400, true},
		{instances[0].Id, start + 20, 20, 400, true},
		{instances[1].Id, start + 30, 5, 800, true},
		{instances[0].Id, start + 70, 30, 0, false},
	} {
		var err error
		if sample.hasCapacity {
			err = recordConductorRPMSample(ctx, sample.instanceID, sample.at, sample.rpm, sample.capacity)
		} else {
			err = recordConductorRPMSample(ctx, sample.instanceID, sample.at, sample.rpm)
		}
		if err != nil {
			t.Fatalf("record sample: %v", err)
		}
	}

	minute, err := GetConductorRPMHistory(ctx, []int64{instances[0].Id, instances[1].Id}, ConductorRPMBucketMinute, start, start+120)
	if err != nil {
		t.Fatalf("minute history: %v", err)
	}
	if len(minute.Points) != 2 || minute.Points[0].RPM != 20 || minute.Points[1].RPM != 30 {
		t.Fatalf("minute points = %#v, want RPM 20 and 30", minute.Points)
	}
	if minute.Points[0].Capacity == nil || *minute.Points[0].Capacity != 1200 || minute.Points[1].Capacity != nil {
		t.Fatalf("minute capacities = %#v, want 1200 then nil", minute.Points)
	}

	hour, err := GetConductorRPMHistory(ctx, []int64{instances[0].Id, instances[1].Id}, ConductorRPMBucketHour, start, start+3600)
	if err != nil {
		t.Fatalf("hour history: %v", err)
	}
	if len(hour.Points) != 1 || hour.Points[0].RPM != 25 {
		t.Fatalf("hour points = %#v, want RPM 25", hour.Points)
	}
	if hour.Points[0].Capacity == nil || *hour.Points[0].Capacity != 1200 {
		t.Fatalf("hour capacity = %#v, want 1200", hour.Points[0].Capacity)
	}
}

func TestManagedRealtimeHistoryAggregatesClaudeGatewaySuccessRate(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instances := []model.ManagedInstance{
		{Name: "gateway-one", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway-one.example.com"},
		{Name: "gateway-two", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway-two.example.com"},
	}
	if err := db.Create(&instances).Error; err != nil {
		t.Fatalf("create instances: %v", err)
	}
	start := int64(1_786_593_600)
	ctx := context.Background()
	samples := []struct {
		instanceID int64
		at         int64
		rpm        float64
		rate       float64
		weight     float64
	}{
		{instances[0].Id, start + 10, 10, 0.8, 100},
		{instances[0].Id, start + 20, 20, 0.6, 100},
		{instances[1].Id, start + 30, 5, 0.5, 200},
		{instances[0].Id, start + 70, 30, 0.9, 100},
	}
	for _, sample := range samples {
		rate := sample.rate
		if err := RecordManagedRealtimeSample(ctx, sample.instanceID, sample.at, sample.rpm, &rate, sample.weight); err != nil {
			t.Fatalf("record sample: %v", err)
		}
	}

	minute, err := GetManagedRPMHistory(ctx, []int64{instances[0].Id, instances[1].Id}, ConductorRPMBucketMinute, start, start+120)
	if err != nil {
		t.Fatalf("minute history: %v", err)
	}
	if len(minute.Points) != 2 || minute.Points[0].SuccessRate == nil || minute.Points[1].SuccessRate == nil {
		t.Fatalf("minute points = %#v, want two success-rate points", minute.Points)
	}
	if math.Abs(*minute.Points[0].SuccessRate-0.6) > 1e-9 || math.Abs(*minute.Points[1].SuccessRate-0.9) > 1e-9 {
		t.Fatalf("minute success rates = %#v, want 0.6 and 0.9", minute.Points)
	}

	hour, err := GetManagedRPMHistory(ctx, []int64{instances[0].Id, instances[1].Id}, ConductorRPMBucketHour, start, start+3600)
	if err != nil {
		t.Fatalf("hour history: %v", err)
	}
	if len(hour.Points) != 1 || hour.Points[0].SuccessRate == nil || math.Abs(*hour.Points[0].SuccessRate-0.66) > 1e-9 {
		t.Fatalf("hour points = %#v, want weighted success rate 0.66", hour.Points)
	}
}

func TestManagedRealtimeHistoryUsesLastCompleteGatewayAccountValues(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instances := []model.ManagedInstance{
		{Name: "gateway-one", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway-one.example.com"},
		{Name: "gateway-two", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway-two.example.com"},
	}
	require.NoError(t, db.Create(&instances).Error)
	start := int64(1_786_593_600)
	ctx := context.Background()

	record := func(instanceID, at int64, rpm float64, available, total int) {
		t.Helper()
		require.NoError(t, RecordManagedRealtimeSampleWithAccounts(ctx, instanceID, at, rpm, nil, 0, available, total))
	}
	record(instances[0].Id, start+10, 10, 2, 10)
	record(instances[0].Id, start+20, 20, 3, 11)
	record(instances[1].Id, start+30, 5, 4, 12)
	record(instances[0].Id, start+70, 30, 0, 0)
	record(instances[1].Id, start+130, 40, 6, 14)
	record(instances[0].Id, start+190, 50, 7, 15)
	record(instances[1].Id, start+250, 60, 8, 16)

	minute, err := GetManagedRPMHistory(ctx, []int64{instances[0].Id, instances[1].Id}, ConductorRPMBucketMinute, start, start+300)
	require.NoError(t, err)
	require.Len(t, minute.Points, 5)
	require.NotNil(t, minute.Points[0].AccountsAvailable)
	require.NotNil(t, minute.Points[0].AccountsTotal)
	require.Equal(t, 7, *minute.Points[0].AccountsAvailable)
	require.Equal(t, 23, *minute.Points[0].AccountsTotal)
	require.Equal(t, 3, minute.Points[0].AccountSamples)
	require.Nil(t, minute.Points[1].AccountsAvailable)
	require.Nil(t, minute.Points[1].AccountsTotal)
	require.Equal(t, 1, minute.Points[1].AccountSamples)

	hour, err := GetManagedRPMHistory(ctx, []int64{instances[0].Id, instances[1].Id}, ConductorRPMBucketHour, start, start+3600)
	require.NoError(t, err)
	require.Len(t, hour.Points, 1)
	require.NotNil(t, hour.Points[0].AccountsAvailable)
	require.NotNil(t, hour.Points[0].AccountsTotal)
	require.Equal(t, 15, *hour.Points[0].AccountsAvailable)
	require.Equal(t, 31, *hour.Points[0].AccountsTotal)
	require.Equal(t, 7, hour.Points[0].AccountSamples)
}

func TestManagedRealtimeHistoryRejectsInvalidGatewayAccountValues(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instance := model.ManagedInstance{Name: "gateway", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway.example.com"}
	require.NoError(t, db.Create(&instance).Error)

	err := RecordManagedRealtimeSampleWithAccounts(context.Background(), instance.Id, 1_786_593_610, 10, nil, 0, 2, 1)
	require.ErrorIs(t, err, ErrInvalidInstance)
	var count int64
	require.NoError(t, db.Model(&model.ManagedRPMHistory{}).Where("instance_id = ?", instance.Id).Count(&count).Error)
	require.Zero(t, count)
}

func TestManagedRealtimeHistoryStoresAuxiliaryMetricsAndRealZero(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instance := model.ManagedInstance{Name: "gateway", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway.example.com"}
	require.NoError(t, db.Create(&instance).Error)
	start := int64(1_788_000_000)
	rpm, used, maximum, cost, cost7D, cost30D := 10.0, 3.0, 20.0, 4.25, 21.5, 88.75
	available, total, sessions := 2, 5, 7
	require.NoError(t, RecordManagedRealtimeHistorySample(t.Context(), instance.Id, start+10, ManagedRealtimeHistorySample{
		RPM: &rpm, ConcurrencyUsed: &used, ConcurrencyMax: &maximum, TodayCost: &cost, Cost7D: &cost7D, Cost30D: &cost30D,
		AccountsAvailable: &available, AccountsTotal: &total, ActiveSessions: &sessions,
	}))
	zero, zeroCost, zeroSessions := 0.0, 0.0, 0
	require.NoError(t, RecordManagedRealtimeHistorySample(t.Context(), instance.Id, start+20, ManagedRealtimeHistorySample{
		RPM: &rpm, ConcurrencyUsed: &zero, ConcurrencyMax: &maximum, TodayCost: &zeroCost, Cost7D: &zeroCost, Cost30D: &zeroCost,
		AccountsAvailable: &available, AccountsTotal: &total, ActiveSessions: &zeroSessions,
	}))

	history, err := GetManagedRPMHistory(t.Context(), []int64{instance.Id}, ConductorRPMBucketMinute, start, start+59)
	require.NoError(t, err)
	require.Len(t, history.Points, 1)
	point := history.Points[0]
	require.NotNil(t, point.ConcurrencyUsed)
	require.Zero(t, *point.ConcurrencyUsed)
	require.Equal(t, 20.0, *point.ConcurrencyMax)
	require.NotNil(t, point.TodayCost)
	require.Zero(t, *point.TodayCost)
	require.NotNil(t, point.ActiveSessions)
	require.Zero(t, *point.ActiveSessions)
	require.Equal(t, 2, point.ConcurrencySamples)
	require.Equal(t, 2, point.TodayCostSamples)
	require.True(t, point.TodayCostComplete)
	require.Equal(t, 2, point.ActiveSessionSamples)
	var stored model.ManagedRPMHistory
	require.NoError(t, db.Where("instance_id = ?", instance.Id).First(&stored).Error)
	require.Zero(t, stored.Cost7DLast)
	require.Zero(t, stored.Cost30DLast)
	require.Equal(t, 2, stored.Cost7DSampleCount)
	require.Equal(t, 2, stored.Cost30DSampleCount)
}

func TestManagedRealtimeHistoryDayBucketsUseShanghaiMidnight(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instance := model.ManagedInstance{Name: "gateway", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway.example.com"}
	require.NoError(t, db.Create(&instance).Error)
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	dayStart := time.Date(2026, 8, 28, 0, 0, 0, 0, location).Unix()
	require.NoError(t, RecordManagedRPMSample(t.Context(), instance.Id, dayStart+60, 20))
	require.NoError(t, RecordManagedRPMSample(t.Context(), instance.Id, dayStart+3600, 40))
	earlyCost, finalCost := 2.5, 9.75
	require.NoError(t, RecordManagedRealtimeHistorySample(t.Context(), instance.Id, dayStart+120, ManagedRealtimeHistorySample{TodayCost: &earlyCost}))
	require.NoError(t, RecordManagedRealtimeHistorySample(t.Context(), instance.Id, dayStart+7200, ManagedRealtimeHistorySample{TodayCost: &finalCost}))

	history, err := GetManagedRPMHistory(t.Context(), []int64{instance.Id}, ConductorRPMBucketDay, dayStart, dayStart+86399)
	require.NoError(t, err)
	require.Len(t, history.Points, 1)
	require.Equal(t, dayStart, history.Points[0].Timestamp)
	require.Equal(t, 30.0, history.Points[0].RPM)
	require.Equal(t, finalCost, *history.Points[0].TodayCost)
	require.True(t, history.Points[0].TodayCostComplete)
}

func TestLatestManagedRealtimeLoadsEachCostFromItsLatestSuccessfulSample(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instance := model.ManagedInstance{Name: "gateway-cost-cache", Kind: model.ManagedInstanceKindClaudeGateway, BaseURL: "https://gateway.example.com"}
	require.NoError(t, db.Create(&instance).Error)
	now := time.Now().Unix()
	todayOld, cost7D, cost30D := 1.0, 7.0, 30.0
	require.NoError(t, RecordManagedRealtimeHistorySample(t.Context(), instance.Id, now-120, ManagedRealtimeHistorySample{
		TodayCost: &todayOld, Cost7D: &cost7D, Cost30D: &cost30D,
	}))
	todayLatest := 2.5
	require.NoError(t, RecordManagedRealtimeHistorySample(t.Context(), instance.Id, now, ManagedRealtimeHistorySample{TodayCost: &todayLatest}))

	state, ok := latestManagedRPMState(instance.Id)
	require.True(t, ok)
	require.Equal(t, todayLatest, *state.TodayCost.Value)
	require.Equal(t, cost7D, *state.Cost7D.Value)
	require.Equal(t, cost30D, *state.Cost30D.Value)
}

func TestManagedRPMHistoryRejectsGenericInstances(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instance := model.ManagedInstance{Name: "generic", Kind: model.ManagedInstanceKindGeneric, BaseURL: "https://generic.example.com"}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	_, err := GetConductorRPMHistory(context.Background(), []int64{instance.Id}, ConductorRPMBucketMinute, 1, 60)
	if err != ErrUnsupportedCapability {
		t.Fatalf("error = %v, want ErrUnsupportedCapability", err)
	}
}

func TestManagedRPMHistorySupportsPollingPlatformsWithoutCapacity(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instances := []model.ManagedInstance{
		{Name: "sub2", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://sub2.example.com"},
		{Name: "new-api", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://new.example.com"},
		{Name: "huichuan", Kind: model.ManagedInstanceKindHuichuan, BaseURL: "https://huichuan.example.com"},
	}
	if err := db.Create(&instances).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	start := int64(1_786_593_600)
	for index, instance := range instances {
		if err := RecordManagedRPMSample(context.Background(), instance.Id, start+10, float64(40+index)); err != nil {
			t.Fatalf("record sample: %v", err)
		}
	}

	history, err := GetManagedRPMHistory(context.Background(), []int64{instances[0].Id, instances[1].Id, instances[2].Id}, ConductorRPMBucketMinute, start, start+60)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history.Points) != 1 || history.Points[0].RPM != 123 {
		t.Fatalf("points = %#v, want one aggregated 123 RPM point", history.Points)
	}
	if history.Points[0].Capacity != nil {
		t.Fatalf("capacity = %#v, want nil", history.Points[0].Capacity)
	}
}
