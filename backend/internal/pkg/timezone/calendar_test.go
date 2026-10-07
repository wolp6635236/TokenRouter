package timezone

import (
	"reflect"
	"testing"
	"time"
)

// TestCalendarKeepsExplicitLocation 验证不同时区对象互不影响，并保持夏令时日界。
func TestCalendarKeepsExplicitLocation(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	calendar := NewCalendar(loc)
	day := time.Date(2026, 3, 8, 12, 0, 0, 0, loc)
	start := calendar.StartOfDay(day)
	end := calendar.EndOfDay(day)
	if end.Add(time.Nanosecond).Sub(start) != 23*time.Hour {
		t.Fatalf("DST day bounds: %s %s", start, end)
	}
	if NewCalendar(time.UTC).StartOfDay(day).Equal(start) {
		t.Fatal("independent calendars must keep different day boundaries")
	}
	parsed, err := calendar.ParseInUserLocation("2006-01-02", "2026-03-08", "invalid/timezone")
	if err != nil || !parsed.Equal(start) {
		t.Fatalf("invalid user timezone fallback: %s %v", parsed, err)
	}
	if calendar.StartOfWeek(day).Weekday() != time.Monday {
		t.Fatal("week must start on Monday")
	}
}

// TestUninitializedClockKeepsMonotonicReading 检查初始化前 Now 与用户时区回退仍保有单调时钟读数。
func TestUninitializedClockKeepsMonotonicReading(t *testing.T) {
	calendar := NewCalendar(nil)
	for name, value := range map[string]time.Time{
		"empty_user":   calendar.NowInUserLocation(""),
		"invalid_user": calendar.NowInUserLocation("invalid/timezone"),
		"calendar":     calendar.Now(),
	} {
		// 这里比较完整 Time 值；Equal 不区分是否携带单调时钟读数。
		if reflect.DeepEqual(value, value.Round(0)) {
			t.Errorf("%s: initialization fallback must preserve monotonic time", name)
		}
	}
}

// TestCalendarInjectedClock 检查固定时钟用于日界线及用户时区转换。
func TestCalendarInjectedClock(t *testing.T) {
	fixed := time.Date(2026, time.October, 5, 23, 30, 0, 0, time.UTC)
	loc := time.FixedZone("UTC+8", 8*60*60)
	calendar := NewCalendarWithClock(loc, func() time.Time { return fixed })
	if got := calendar.Today(); got.Day() != 6 || got.Hour() != 0 {
		t.Fatalf("日界线计算错误: %v", got)
	}
	if got := calendar.NowInUserLocation("UTC"); !got.Equal(fixed) || got.Location() != time.UTC {
		t.Fatalf("用户时区未使用固定时钟: %v", got)
	}
	if got := NewCalendarWithClock(nil, func() time.Time { return fixed }).Now(); got != fixed {
		t.Fatalf("未指定时区时应保留时钟返回值: %v", got)
	}
	if got := NewCalendarWithClock(nil, nil).Now(); time.Since(got) > time.Second {
		t.Fatalf("nil 时钟应使用系统时间: %v", got)
	}
}
