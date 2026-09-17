package accountexport

import (
	"testing"
	"time"
)

func TestNextSchedule(t *testing.T) {
	tests := []struct {
		name        string
		schedule    Schedule
		after, want string
	}{
		{"daily UTC boundary", Schedule{Kind: "daily", Hour: 0}, "2026-09-17T15:59:59Z", "2026-09-17T16:00:00Z"},
		{"exact time is not repeated", Schedule{Kind: "daily", Hour: 0}, "2026-09-17T16:00:00Z", "2026-09-18T16:00:00Z"},
		{"weekly Sunday", Schedule{Kind: "weekly", Weekday: 0, Hour: 9, Minute: 30}, "2026-09-17T00:00:00Z", "2026-09-20T01:30:00Z"},
		{"weekly skips exact slot", Schedule{Kind: "weekly", Weekday: 0, Hour: 9, Minute: 30}, "2026-09-20T01:30:00Z", "2026-09-27T01:30:00Z"},
		{"interval preserves anchor", Schedule{Kind: "interval", IntervalHours: 3, FirstAt: 1789603200}, "2026-09-17T10:45:00Z", "2026-09-17T12:00:00Z"},
		{"future anchor", Schedule{Kind: "interval", IntervalHours: 3, FirstAt: 1789603200}, "2026-09-16T00:00:00Z", "2026-09-17T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			after, err := time.Parse(time.RFC3339, tt.after)
			if err != nil {
				t.Fatal(err)
			}
			got, err := tt.schedule.Next(after)
			if err != nil {
				t.Fatal(err)
			}
			if got.UTC().Format(time.RFC3339) != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestInvalidSchedules(t *testing.T) {
	for _, schedule := range []Schedule{
		{}, {Kind: "daily", Hour: 24}, {Kind: "daily", Minute: -1},
		{Kind: "weekly", Weekday: 7}, {Kind: "interval", IntervalHours: 0, FirstAt: 1},
		{Kind: "interval", IntervalHours: 1}, {Kind: "interval", IntervalHours: 3000000, FirstAt: 1},
	} {
		if _, err := schedule.Next(time.Now()); err == nil {
			t.Fatalf("accepted %+v", schedule)
		}
	}
}

func TestDeliveryRetryLimit(t *testing.T) {
	for i, want := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute} {
		got, ok := DeliveryRetryDelay(i)
		if !ok || got != want {
			t.Fatalf("retry %d: %v %v", i, got, ok)
		}
	}
	for _, i := range []int{-1, 3, 4} {
		if _, ok := DeliveryRetryDelay(i); ok {
			t.Fatalf("accepted retry %d", i)
		}
	}
}
