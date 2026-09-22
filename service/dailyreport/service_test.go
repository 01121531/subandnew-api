package dailyreport

import (
	"testing"
	"time"
)

func TestNaturalDayUsesBeijingBoundaries(t *testing.T) {
	window, err := NaturalDay("2026-09-22")
	if err != nil {
		t.Fatal(err)
	}
	loc := BeijingLocation()
	start := time.Unix(window.Start, 0).In(loc)
	end := time.Unix(window.End, 0).In(loc)
	if start.Format("2006-01-02 15:04:05") != "2026-09-22 00:00:00" {
		t.Fatalf("unexpected start: %s", start)
	}
	if end.Format("2006-01-02 15:04:05") != "2026-09-22 23:59:59" {
		t.Fatalf("unexpected end: %s", end)
	}
}

func TestRollingWindowKeepsShanghaiDate(t *testing.T) {
	end := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	window := Rolling(end, 6*time.Hour)
	if window.Mode != "rolling" || window.Date != "2026-09-22" {
		t.Fatalf("unexpected rolling window: %+v", window)
	}
	if window.End-window.Start != int64(6*time.Hour/time.Second) {
		t.Fatalf("unexpected duration: %d", window.End-window.Start)
	}
}
