package timezone

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Calendar 保存日期计算使用的时区和时钟。
type Calendar struct {
	location *time.Location
	now      func() time.Time
}

// NewCalendar 使用指定时区，nil 表示 time.Local。
func NewCalendar(loc *time.Location) Calendar {
	return Calendar{location: loc}
}

// NewCalendarWithClock 使用调用方提供的时钟，传 nil 时读取系统时间。
func NewCalendarWithClock(loc *time.Location, now func() time.Time) Calendar {
	return Calendar{location: loc, now: now}
}

// Location 返回本对象的时区，零值使用 time.Local。
func (c Calendar) Location() *time.Location {
	if c.location == nil {
		return time.Local
	}
	return c.location
}

// Now 返回日历时区的当前时间。零值直接返回系统时钟的时间和单调读数。
func (c Calendar) Now() time.Time {
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	value := now()
	if c.location == nil {
		return value
	}
	return value.In(c.Location())
}

// UTCOffset 返回给定时刻在当前时区的 UTC 偏移文本。
func (c Calendar) UTCOffset(t time.Time) string {
	_, offset := t.In(c.Location()).Zone()
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	if minutes < 0 {
		minutes = -minutes
	}
	sign := "+"
	if hours < 0 {
		sign = "-"
		hours = -hours
	}
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}

// StartOfDay 返回给定时刻在日历时区中的零点。
func (c Calendar) StartOfDay(t time.Time) time.Time {
	loc := c.Location()
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// Today 返回日历时区中当天的零点。
func (c Calendar) Today() time.Time {
	return c.StartOfDay(c.Now())
}

// EndOfDay 返回给定日期的最后一纳秒。
func (c Calendar) EndOfDay(t time.Time) time.Time {
	loc := c.Location()
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, loc)
}

// StartOfWeek 返回所在周的星期一零点。
func (c Calendar) StartOfWeek(t time.Time) time.Time {
	loc := c.Location()
	t = t.In(loc)
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // 星期日按一周中的第七天计算。
	}
	return time.Date(t.Year(), t.Month(), t.Day()-weekday+1, 0, 0, 0, 0, loc)
}

// StartOfMonth 返回所在月份的第一天零点。
func (c Calendar) StartOfMonth(t time.Time) time.Time {
	loc := c.Location()
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
}

// ParseInUserLocation 按用户时区解析日期，时区无效时使用日历时区。
func (c Calendar) ParseInUserLocation(layout, value, userTZ string) (time.Time, error) {
	loc := c.Location() // 默认使用日历时区。
	if userTZ != "" {
		if userLoc, err := time.LoadLocation(userTZ); err == nil {
			loc = userLoc
		}
	}
	return time.ParseInLocation(layout, value, loc)
}

// ParseDateTimeInUserLocation 解析日期或时间，并报告输入是否只有日期。
func (c Calendar) ParseDateTimeInUserLocation(value, userTZ string) (parsed time.Time, dateOnly bool, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false, fmt.Errorf("empty datetime")
	}

	if t, parseErr := time.Parse(time.RFC3339Nano, value); parseErr == nil {
		return t, false, nil
	}

	loc := c.Location()
	if userTZ != "" {
		if userLoc, loadErr := time.LoadLocation(userTZ); loadErr == nil {
			loc = userLoc
		}
	}

	layouts := []struct {
		layout   string
		dateOnly bool
	}{
		{layout: "2006-01-02", dateOnly: true},
		{layout: "2006-01-02T15:04:05", dateOnly: false},
		{layout: "2006-01-02T15:04", dateOnly: false},
		{layout: "2006-01-02 15:04:05", dateOnly: false},
		{layout: "2006-01-02 15:04", dateOnly: false},
	}
	for _, candidate := range layouts {
		if t, parseErr := time.ParseInLocation(candidate.layout, value, loc); parseErr == nil {
			return t, candidate.dateOnly, nil
		}
	}

	return time.Time{}, false, fmt.Errorf("invalid datetime %q", value)
}

// NowInUserLocation 返回用户时区的当前时间，时区无效时使用日历时区。
func (c Calendar) NowInUserLocation(userTZ string) time.Time {
	if userTZ == "" {
		return c.Now()
	}
	if userLoc, err := time.LoadLocation(userTZ); err == nil {
		return c.Now().In(userLoc)
	}
	return c.Now()
}

// StartOfDayInUserLocation 返回用户时区中的零点，时区无效时使用日历时区。
func (c Calendar) StartOfDayInUserLocation(t time.Time, userTZ string) time.Time {
	loc := c.Location()
	if userTZ != "" {
		if userLoc, err := time.LoadLocation(userTZ); err == nil {
			loc = userLoc
		}
	}
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// ParseFlexibleTimestamp 尝试 RFC3339、RFC3339Nano 和 UTC 时间戳格式。
func ParseFlexibleTimestamp(raw string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
	}
	for _, format := range formats {
		if ts, err := time.Parse(format, raw); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, strconv.ErrSyntax
}
