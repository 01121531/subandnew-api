package billingalert

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	CycleNaturalDay   = "natural_day"
	CycleNaturalWeek  = "natural_week"
	CycleNaturalMonth = "natural_month"
	CycleMonthlyDay   = "monthly_day"
	CycleFixed        = "fixed"
	CycleRollingDays  = "rolling_days"
)

var ErrInvalidCycle = errors.New("invalid billing cycle")

type CycleConfig struct {
	DayOfMonth int   `json:"day_of_month"`
	Hour       int   `json:"hour"`
	Minute     int   `json:"minute"`
	Start      int64 `json:"start"`
	End        int64 `json:"end"`
	Anchor     int64 `json:"anchor"`
	Days       int   `json:"days"`
}

type CycleWindow struct {
	Key      string `json:"key"`
	Start    int64  `json:"start"`
	End      int64  `json:"end"`
	Timezone string `json:"timezone"`
	Active   bool   `json:"active"`
}

func ResolveCycle(now time.Time, timezone string, cycleType string, rawConfig string) (CycleWindow, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return CycleWindow{}, fmt.Errorf("%w: timezone", ErrInvalidCycle)
	}
	localNow := now.In(location)
	config := CycleConfig{}
	if rawConfig != "" && rawConfig != "{}" {
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			return CycleWindow{}, fmt.Errorf("%w: config", ErrInvalidCycle)
		}
	}
	var start, end time.Time
	switch cycleType {
	case CycleNaturalDay:
		start = time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, location)
		end = start.AddDate(0, 0, 1)
	case CycleNaturalWeek:
		weekdayOffset := (int(localNow.Weekday()) + 6) % 7
		start = time.Date(localNow.Year(), localNow.Month(), localNow.Day()-weekdayOffset, 0, 0, 0, 0, location)
		end = start.AddDate(0, 0, 7)
	case CycleNaturalMonth:
		start = time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, location)
		end = start.AddDate(0, 1, 0)
	case CycleMonthlyDay:
		if config.DayOfMonth < 1 || config.DayOfMonth > 31 || !validClock(config.Hour, config.Minute) {
			return CycleWindow{}, ErrInvalidCycle
		}
		start = clampedMonthTime(localNow.Year(), localNow.Month(), config, location)
		if localNow.Before(start) {
			previous := localNow.AddDate(0, -1, 0)
			start = clampedMonthTime(previous.Year(), previous.Month(), config, location)
		}
		next := start.AddDate(0, 1, 0)
		end = clampedMonthTime(next.Year(), next.Month(), config, location)
	case CycleFixed:
		if config.Start <= 0 || config.End <= config.Start {
			return CycleWindow{}, ErrInvalidCycle
		}
		start = time.Unix(config.Start, 0).In(location)
		end = time.Unix(config.End, 0).In(location)
	case CycleRollingDays:
		if config.Anchor <= 0 || config.Days <= 0 || config.Days > 3660 {
			return CycleWindow{}, ErrInvalidCycle
		}
		anchor := time.Unix(config.Anchor, 0).In(location)
		if localNow.Before(anchor) {
			start = anchor
			end = anchor.AddDate(0, 0, config.Days)
			break
		}
		periodSeconds := int64(config.Days * 24 * 60 * 60)
		periods := (localNow.Unix() - anchor.Unix()) / periodSeconds
		start = anchor.AddDate(0, 0, int(periods)*config.Days)
		for !localNow.Before(start.AddDate(0, 0, config.Days)) {
			start = start.AddDate(0, 0, config.Days)
		}
		end = start.AddDate(0, 0, config.Days)
	default:
		return CycleWindow{}, ErrInvalidCycle
	}
	active := !localNow.Before(start) && localNow.Before(end)
	return CycleWindow{
		Key:   start.Format("20060102T150405-0700") + "_" + end.Format("20060102T150405-0700"),
		Start: start.Unix(), End: end.Unix(), Timezone: timezone, Active: active,
	}, nil
}

func validClock(hour int, minute int) bool {
	return hour >= 0 && hour <= 23 && minute >= 0 && minute <= 59
}

func clampedMonthTime(year int, month time.Month, config CycleConfig, location *time.Location) time.Time {
	firstNextMonth := time.Date(year, month, 1, config.Hour, config.Minute, 0, 0, location).AddDate(0, 1, 0)
	lastDay := firstNextMonth.AddDate(0, 0, -1).Day()
	day := config.DayOfMonth
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, month, day, config.Hour, config.Minute, 0, 0, location)
}
