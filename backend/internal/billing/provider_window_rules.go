package billing

import (
	"time"
)

const (
	ProviderDailyWindow ProviderWindowPeriod = iota
	ProviderWeeklyWindow
)

// ProviderWindowPeriod 区分提供商的日历日窗口和日历周窗口。
type ProviderWindowPeriod uint8

type FixedProviderWindow struct {
	Period    ProviderWindowPeriod
	Mode      string
	Limit     float64
	Hour, Day int
}

// NextFixedDailyReset 计算在 after 之后的下一个每日固定重置时间点。
func NextFixedDailyReset(hour int, tz *time.Location, after time.Time) time.Time {
	t := after.In(tz)
	today := time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, tz)
	if !after.Before(today) {
		return today.AddDate(0, 0, 1)
	}
	return today
}

// LastFixedDailyReset 计算 now 之前最近一次的每日固定重置时间点。
func LastFixedDailyReset(hour int, tz *time.Location, now time.Time) time.Time {
	t := now.In(tz)
	today := time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, tz)
	if now.Before(today) {
		return today.AddDate(0, 0, -1)
	}
	return today
}

// NextFixedWeeklyReset 计算在 after 之后的下一个每周固定重置时间点
// day：0 为周日，1 为周一，依次到 6 为周六。
func NextFixedWeeklyReset(day, hour int, tz *time.Location, after time.Time) time.Time {
	t := after.In(tz)
	todayReset := time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, tz)
	currentDay := int(todayReset.Weekday())

	daysForward := (day - currentDay + 7) % 7
	if daysForward == 0 && !after.Before(todayReset) {
		daysForward = 7
	}
	return todayReset.AddDate(0, 0, daysForward)
}

// LastFixedWeeklyReset 计算 now 之前最近一次的每周固定重置时间点。
func LastFixedWeeklyReset(day, hour int, tz *time.Location, now time.Time) time.Time {
	t := now.In(tz)
	todayReset := time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, tz)
	currentDay := int(todayReset.Weekday())

	daysBack := (currentDay - day + 7) % 7
	if daysBack == 0 && now.Before(todayReset) {
		daysBack = 7
	}
	return todayReset.AddDate(0, 0, -daysBack)
}

func (w FixedProviderWindow) normalized() FixedProviderWindow {
	if w.Hour < 0 || w.Hour > 23 {
		w.Hour = 0
	}
	if w.Day < 0 || w.Day > 6 {
		w.Day = 1
	}
	return w
}

// NextProviderFixedReset 计算固定窗口的下一次重置时间，其他模式返回零值和 false。
func NextProviderFixedReset(window FixedProviderWindow, tz *time.Location, now time.Time) (time.Time, bool) {
	if window.Mode != "fixed" {
		return time.Time{}, false
	}
	window = window.normalized()
	if window.Period == ProviderWeeklyWindow {
		return NextFixedWeeklyReset(window.Day, window.Hour, tz, now), true
	}
	return NextFixedDailyReset(window.Hour, tz, now), true
}

// ReconcileProviderFixedWindow 判断提供商窗口是否到期并返回重置时间，由调用方交给存储写入。
func ReconcileProviderFixedWindow(window FixedProviderWindow, start time.Time, tz *time.Location, now time.Time) (time.Time, bool) {
	if window.Mode != "fixed" || !(window.Limit > 0) {
		return time.Time{}, false
	}
	window = window.normalized()
	var last time.Time
	if window.Period == ProviderWeeklyWindow {
		last = LastFixedWeeklyReset(window.Day, window.Hour, tz, now)
	} else {
		last = LastFixedDailyReset(window.Hour, tz, now)
	}
	return last, start.IsZero() || start.Before(last)
}
