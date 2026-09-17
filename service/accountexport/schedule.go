package accountexport

import (
	"errors"
	"time"
	_ "time/tzdata"
)

var ErrInvalidSchedule = errors.New("invalid export schedule")

// Schedule uses Shanghai wall time for calendar schedules and an immutable
// anchor for intervals. Next always skips missed occurrences.
type Schedule struct {
	Kind          string `json:"kind"`
	Hour          int    `json:"hour"`
	Minute        int    `json:"minute"`
	Weekday       int    `json:"weekday"`
	IntervalHours int    `json:"interval_hours"`
	FirstAt       int64  `json:"first_at"`
}

func (s Schedule) Validate() error {
	switch s.Kind {
	case "daily", "weekly":
		if s.Hour < 0 || s.Hour > 23 || s.Minute < 0 || s.Minute > 59 {
			return ErrInvalidSchedule
		}
		if s.Kind == "weekly" && (s.Weekday < 0 || s.Weekday > 6) {
			return ErrInvalidSchedule
		}
	case "interval":
		if s.IntervalHours < 1 || int64(s.IntervalHours) > (1<<63-1)/int64(time.Hour) || s.FirstAt <= 0 {
			return ErrInvalidSchedule
		}
	default:
		return ErrInvalidSchedule
	}
	return nil
}

func (s Schedule) Next(after time.Time) (time.Time, error) {
	if err := s.Validate(); err != nil {
		return time.Time{}, err
	}
	if s.Kind == "interval" {
		anchor := time.Unix(s.FirstAt, 0)
		if anchor.After(after) {
			return anchor, nil
		}
		interval := time.Duration(s.IntervalHours) * time.Hour
		return anchor.Add((after.Sub(anchor)/interval + 1) * interval), nil
	}
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Time{}, err
	}
	local := after.In(zone)
	next := time.Date(local.Year(), local.Month(), local.Day(), s.Hour, s.Minute, 0, 0, zone)
	if s.Kind == "weekly" {
		next = next.AddDate(0, 0, (s.Weekday-int(next.Weekday())+7)%7)
		if !next.After(after) {
			next = next.AddDate(0, 0, 7)
		}
	} else if !next.After(after) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}

// DeliveryRetryDelay is used only for explicitly rejected temporary deliveries.
func DeliveryRetryDelay(retries int) (time.Duration, bool) {
	delays := [...]time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}
	if retries < 0 || retries >= len(delays) {
		return 0, false
	}
	return delays[retries], true
}
