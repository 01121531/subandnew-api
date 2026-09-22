package dailyreport

import (
	"encoding/json"
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

func TestValidateDateRangeAllowsThirtyOneDaysOnly(t *testing.T) {
	if _, _, err := ValidateDateRange("2026-09-01", "2026-10-01"); err != nil {
		t.Fatalf("31-day range should be accepted: %v", err)
	}
	if _, _, err := ValidateDateRange("2026-09-01", "2026-10-02"); err == nil {
		t.Fatal("32-day range should be rejected")
	}
}

func TestRouterBillPreservesAmountsAndTokenDetails(t *testing.T) {
	row := map[string]any{
		"supplier": "供应商 A", "date": "2026-09-22T00:00:00+08:00",
		"request_count": 12, "prompt_tokens": 100, "completion_tokens": 25,
		"cache_read_tokens": 5, "raw_amount": "1.25", "actual_settlement_amount": "0.75",
		"channel_count": 3,
	}
	item := routerBill(row, 7, "2026-09-22")
	if item.SupplierName != "供应商 A" || item.BillDate != "2026-09-22" || item.Requests != 12 {
		t.Fatalf("unexpected bill identity: %+v", item)
	}
	if item.OriginalAmount != 1.25 || item.PayableAmount != 0.75 || item.ChannelCount != 3 {
		t.Fatalf("unexpected bill amounts: %+v", item)
	}
	if item.TokenDetails["prompt_tokens"] != 100 || item.TokenDetails["cache_read_tokens"] != 5 {
		t.Fatalf("unexpected token details: %+v", item.TokenDetails)
	}
}

func TestBuildUploadsOverviewUsesBeijingDays(t *testing.T) {
	items := []UploadRecord{
		{CreatedAt: "2026-09-21T16:30:00Z", BeijingDate: "2026-09-22", Vendor: "A", State: "NEEDS_FIX", Submitted: true},
		{CreatedAt: "2026-09-22T16:30:00Z", BeijingDate: "2026-09-23", Vendor: "B", State: "READY"},
	}
	result := buildUploadsOverview("2026-09-22", "2026-09-23", items)
	if len(result.Days) != 2 || result.Days[0].Date != "2026-09-22" || result.Days[0].NeedsFix != 1 || result.Days[0].Submitted != 1 {
		t.Fatalf("unexpected upload days: %+v", result.Days)
	}
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("upload overview should be serializable: %v", err)
	}
}
