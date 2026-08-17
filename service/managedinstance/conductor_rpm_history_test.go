package managedinstance

import (
	"context"
	"testing"

	"github.com/01121531/subandnew-api/model"
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

func TestConductorRPMHistoryRejectsNonConductorInstances(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instance := model.ManagedInstance{Name: "new-api", Kind: model.ManagedInstanceKindNewAPI, BaseURL: "https://new.example.com"}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	_, err := GetConductorRPMHistory(context.Background(), []int64{instance.Id}, ConductorRPMBucketMinute, 1, 60)
	if err != ErrUnsupportedCapability {
		t.Fatalf("error = %v, want ErrUnsupportedCapability", err)
	}
}

func TestManagedRPMHistorySupportsSub2WithoutCapacity(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	instance := model.ManagedInstance{Name: "sub2", Kind: model.ManagedInstanceKindSub2API, BaseURL: "https://sub2.example.com"}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create instance: %v", err)
	}
	start := int64(1_786_593_600)
	if err := RecordManagedRPMSample(context.Background(), instance.Id, start+10, 42); err != nil {
		t.Fatalf("record sample: %v", err)
	}

	history, err := GetManagedRPMHistory(context.Background(), []int64{instance.Id}, ConductorRPMBucketMinute, start, start+60)
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(history.Points) != 1 || history.Points[0].RPM != 42 {
		t.Fatalf("points = %#v, want one 42 RPM point", history.Points)
	}
	if history.Points[0].Capacity != nil {
		t.Fatalf("capacity = %#v, want nil", history.Points[0].Capacity)
	}
}
