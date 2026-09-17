package accountexport

import (
	"testing"
	"time"
)

func TestReportWindowUsesScheduledShanghaiDate(t *testing.T) {
	scheduled, err := time.Parse(time.RFC3339, "2026-09-17T17:12:00Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ kind, start, end string }{
		{"today", "2026-09-17T16:00:00Z", "2026-09-18T15:59:59Z"},
		{"yesterday", "2026-09-16T16:00:00Z", "2026-09-17T15:59:59Z"},
		{"last7", "2026-09-11T16:00:00Z", "2026-09-18T15:59:59Z"},
		{"last30", "2026-08-19T16:00:00Z", "2026-09-18T15:59:59Z"},
		{"", "2026-08-19T16:00:00Z", "2026-09-18T15:59:59Z"},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			window, err := (ReportPeriod{Kind: tt.kind}).Window(scheduled)
			if err != nil {
				t.Fatal(err)
			}
			if got := time.Unix(window.Start, 0).UTC().Format(time.RFC3339); got != tt.start {
				t.Fatalf("start %s want %s", got, tt.start)
			}
			if got := time.Unix(window.End, 0).UTC().Format(time.RFC3339); got != tt.end {
				t.Fatalf("end %s want %s", got, tt.end)
			}
			if window.Timezone != "Asia/Shanghai" {
				t.Fatal(window.Timezone)
			}
		})
	}
}

func TestFixedReportWindow(t *testing.T) {
	period := ReportPeriod{Kind: "fixed", StartDate: "2024-02-29", EndDate: "2024-02-29"}
	window, err := period.Window(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if window.End-window.Start != 86399 {
		t.Fatalf("window %+v", window)
	}
	for _, period := range []ReportPeriod{
		{Kind: "invalid"},
		{Kind: "fixed", StartDate: "2026-02-29", EndDate: "2026-03-01"},
		{Kind: "fixed", StartDate: "2026-09-18", EndDate: "2026-09-17"},
		{Kind: "fixed"},
	} {
		if _, err := period.Window(time.Now()); err == nil {
			t.Fatalf("accepted %+v", period)
		}
	}
}
