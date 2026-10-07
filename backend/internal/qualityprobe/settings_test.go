package qualityprobe

import (
	"testing"
	"time"
)

func TestWithinScheduleWindow(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 10, 7, hour, minute, 0, 0, loc)
	}
	cases := []struct {
		name       string
		start, end string
		now        time.Time
		want       bool
	}{
		{name: "默认窗口内上午", start: "08:00", end: "00:00", now: at(8, 0), want: true},
		{name: "默认窗口内晚间", start: "08:00", end: "00:00", now: at(23, 59), want: true},
		{name: "默认窗口外凌晨", start: "08:00", end: "00:00", now: at(0, 0), want: false},
		{name: "默认窗口外清晨", start: "08:00", end: "00:00", now: at(7, 59), want: false},
		{name: "同日窗口内", start: "09:00", end: "18:00", now: at(12, 0), want: true},
		{name: "同日窗口外", start: "09:00", end: "18:00", now: at(18, 0), want: false},
		{name: "跨午夜窗口内", start: "22:00", end: "06:00", now: at(23, 0), want: true},
		{name: "跨午夜窗口外", start: "22:00", end: "06:00", now: at(12, 0), want: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := WithinScheduleWindow(tt.start, tt.end, tt.now); got != tt.want {
				t.Fatalf("WithinScheduleWindow(%q,%q,%s)=%v, want %v", tt.start, tt.end, tt.now.Format("15:04"), got, tt.want)
			}
		})
	}
}

func TestAutoScheduleActive(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	previous := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = previous })
	morning := time.Date(2026, 10, 7, 10, 0, 0, 0, loc)
	night := time.Date(2026, 10, 7, 2, 0, 0, 0, loc)
	cfg := DefaultSettings()
	cfg.Enabled = true
	if !AutoScheduleActive(cfg, morning) {
		t.Fatal("default window should cover 10:00")
	}
	if AutoScheduleActive(cfg, night) {
		t.Fatal("default window should skip 02:00")
	}
	cfg.ScheduleEnabled = false
	if !AutoScheduleActive(cfg, night) {
		t.Fatal("disabled schedule runs around the clock")
	}
}

func TestSettingsPayloadPreservesNewFields(t *testing.T) {
	off := false
	payload := SettingsPayload{
		Enabled:              true,
		IntervalMinutes:      30,
		Model:                DefaultProbeModel,
		CooldownMinutes:      5,
		MaxAttempts:          3,
		NotifyEmail:          DefaultNotifyEmail,
		ScheduleEnabled:      &off,
		ScheduleStart:        "09:30",
		ScheduleEnd:          "21:00",
		UnscheduleOnDegraded: &off,
	}
	got := payload.Settings()
	if got.ScheduleEnabled || got.UnscheduleOnDegraded {
		t.Fatalf("explicit false should stick: %+v", got)
	}
	if got.ScheduleStart != "09:30" || got.ScheduleEnd != "21:00" {
		t.Fatalf("clock = %s-%s", got.ScheduleStart, got.ScheduleEnd)
	}
	legacy := SettingsPayload{Enabled: true, IntervalMinutes: 30}.Settings()
	if !legacy.ScheduleEnabled || !legacy.UnscheduleOnDegraded {
		t.Fatal("missing JSON fields keep defaults on")
	}
	if legacy.ScheduleStart != DefaultScheduleStart || legacy.ScheduleEnd != DefaultScheduleEnd {
		t.Fatalf("legacy clocks = %s-%s", legacy.ScheduleStart, legacy.ScheduleEnd)
	}
}

func TestAutoGroupAllowed(t *testing.T) {
	if AutoGroupAllowed(nil, nil) {
		t.Fatal("provider with no groups should skip")
	}
	if !AutoGroupAllowed(nil, []int64{1}) {
		t.Fatal("empty selected should allow grouped providers")
	}
	if !AutoGroupAllowed([]int64{}, []int64{2}) {
		t.Fatal("empty slice should allow grouped providers")
	}
	if !AutoGroupAllowed([]int64{3, 1}, []int64{9, 1}) {
		t.Fatal("intersection should allow")
	}
	if AutoGroupAllowed([]int64{3}, []int64{1, 2}) {
		t.Fatal("no intersection should skip")
	}
	if AutoGroupAllowed([]int64{3}, nil) {
		t.Fatal("provider with no groups should skip when filter is set")
	}
}

func TestAutoScopeAllowedRequiresActiveGroup(t *testing.T) {
	active := map[int64]struct{}{2: {}}
	if AutoScopeAllowed(nil, []int64{1}, active) {
		t.Fatal("disabled-only membership should skip")
	}
	if !AutoScopeAllowed(nil, []int64{1, 2}, active) {
		t.Fatal("one active group should allow")
	}
	if AutoScopeAllowed([]int64{1}, []int64{1, 2}, active) {
		t.Fatal("selected group that is disabled should skip")
	}
}

func TestAccountAutoEligible(t *testing.T) {
	if !AccountAutoEligible(&Snapshot{}) {
		t.Fatal("empty status should treat as active")
	}
	if AccountAutoEligible(&Snapshot{Status: "error"}) {
		t.Fatal("error status should skip")
	}
	if AccountAutoEligible(&Snapshot{SchedulingOff: true}) {
		t.Fatal("scheduling off should skip")
	}
}

func TestNormalizeGroupIDs(t *testing.T) {
	got := NormalizeGroupIDs([]int64{3, 0, 1, 3, -2, 1})
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("got %v", got)
	}
}
