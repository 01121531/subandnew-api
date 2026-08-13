package billingalert

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveNaturalCyclesInConfiguredTimezone(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, 8, 13, 14, 0, 0, 0, location)

	day, err := ResolveCycle(now, "Asia/Shanghai", CycleNaturalDay, "{}")
	require.NoError(t, err)
	require.Equal(t, "2026-08-13 00:00", time.Unix(day.Start, 0).In(location).Format("2006-01-02 15:04"))
	require.Equal(t, "2026-08-14 00:00", time.Unix(day.End, 0).In(location).Format("2006-01-02 15:04"))

	week, err := ResolveCycle(now, "Asia/Shanghai", CycleNaturalWeek, "{}")
	require.NoError(t, err)
	require.Equal(t, "2026-08-10", time.Unix(week.Start, 0).In(location).Format("2006-01-02"))

	month, err := ResolveCycle(now, "Asia/Shanghai", CycleNaturalMonth, "{}")
	require.NoError(t, err)
	require.Equal(t, "2026-08-01", time.Unix(month.Start, 0).In(location).Format("2006-01-02"))
}

func TestResolveMonthlyDayClampsShortMonths(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, 3, 1, 1, 0, 0, 0, location)
	window, err := ResolveCycle(now, "Asia/Shanghai", CycleMonthlyDay, `{"day_of_month":31,"hour":0,"minute":0}`)
	require.NoError(t, err)
	require.Equal(t, "2026-02-28", time.Unix(window.Start, 0).In(location).Format("2006-01-02"))
	require.Equal(t, "2026-03-31", time.Unix(window.End, 0).In(location).Format("2006-01-02"))
}

func TestResolveFixedAndRollingCycles(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	anchor := time.Date(2026, 8, 1, 0, 0, 0, 0, location)
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, location)

	rollingConfig := fmt.Sprintf(`{"anchor":%d,"days":7}`, anchor.Unix())
	rolling, err := ResolveCycle(now, "Asia/Shanghai", CycleRollingDays, rollingConfig)
	require.NoError(t, err)
	require.True(t, rolling.Active)
	require.Equal(t, anchor.AddDate(0, 0, 7).Unix(), rolling.Start)

	fixedConfig := fmt.Sprintf(`{"start":%d,"end":%d}`, anchor.Unix(), anchor.AddDate(0, 1, 0).Unix())
	fixed, err := ResolveCycle(now, "Asia/Shanghai", CycleFixed, fixedConfig)
	require.NoError(t, err)
	require.True(t, fixed.Active)
}

func TestResolveCycleRejectsInvalidConfiguration(t *testing.T) {
	_, err := ResolveCycle(time.Now(), "Invalid/Zone", CycleNaturalDay, "{}")
	require.ErrorIs(t, err, ErrInvalidCycle)
	_, err = ResolveCycle(time.Now(), "Asia/Shanghai", CycleMonthlyDay, `{"day_of_month":0}`)
	require.ErrorIs(t, err, ErrInvalidCycle)
}
