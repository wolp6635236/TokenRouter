package provider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
)

func TestNextFixedDailyReset_BeforeResetHour(t *testing.T) {
	tz := time.UTC
	// 当前时间为 2026-03-14 06:00 UTC，重置时刻为 9:00。
	after := time.Date(2026, 3, 14, 6, 0, 0, 0, tz)
	got := billing.NextFixedDailyReset(9, tz, after)
	want := time.Date(2026, 3, 14, 9, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNextFixedDailyReset_AtResetHour(t *testing.T) {
	tz := time.UTC
	// 当前时刻等于重置时刻，返回明天的重置时间。
	after := time.Date(2026, 3, 14, 9, 0, 0, 0, tz)
	got := billing.NextFixedDailyReset(9, tz, after)
	want := time.Date(2026, 3, 15, 9, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNextFixedDailyReset_AfterResetHour(t *testing.T) {
	tz := time.UTC
	// 已过重置时刻，返回明天的重置时间。
	after := time.Date(2026, 3, 14, 15, 30, 0, 0, tz)
	got := billing.NextFixedDailyReset(9, tz, after)
	want := time.Date(2026, 3, 15, 9, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNextFixedDailyReset_MidnightReset(t *testing.T) {
	tz := time.UTC
	// 每日午夜重置，当前时间为 23:59。
	after := time.Date(2026, 3, 14, 23, 59, 0, 0, tz)
	got := billing.NextFixedDailyReset(0, tz, after)
	want := time.Date(2026, 3, 15, 0, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNextFixedDailyReset_NonUTCTimezone(t *testing.T) {
	tz, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	// 2026-03-14 07:00 UTC 对应上海时间 15:00，重置时刻为当地 9:00。
	after := time.Date(2026, 3, 14, 7, 0, 0, 0, time.UTC)
	got := billing.NextFixedDailyReset(9, tz, after)
	// 已过上海时间 9:00，下次重置为 2026-03-15 01:00 UTC。
	want := time.Date(2026, 3, 15, 9, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNextFixedWeeklyReset_TargetDayAhead(t *testing.T) {
	tz := time.UTC
	// 当前日期为 2026-03-14 星期六，每周一 9:00 重置。
	after := time.Date(2026, 3, 14, 10, 0, 0, 0, tz)
	got := billing.NextFixedWeeklyReset(1, 9, tz, after)
	// 下周一为 2026-03-16。
	want := time.Date(2026, 3, 16, 9, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNextFixedWeeklyReset_TargetDayToday_BeforeHour(t *testing.T) {
	tz := time.UTC
	// 当前为 2026-03-16 星期一，时间早于重置时刻 9:00。
	after := time.Date(2026, 3, 16, 6, 0, 0, 0, tz)
	got := billing.NextFixedWeeklyReset(1, 9, tz, after)
	// 重置时间为今日 9:00。
	want := time.Date(2026, 3, 16, 9, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNextFixedWeeklyReset_TargetDayToday_AtHour(t *testing.T) {
	tz := time.UTC
	// 当前为 2026-03-16 星期一 9:00，恰好等于重置时刻。
	after := time.Date(2026, 3, 16, 9, 0, 0, 0, tz)
	got := billing.NextFixedWeeklyReset(1, 9, tz, after)
	// 下次重置时间为下周一 9:00。
	want := time.Date(2026, 3, 23, 9, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNextFixedWeeklyReset_TargetDayToday_AfterHour(t *testing.T) {
	tz := time.UTC
	// 当前为 2026-03-16 星期一，时间晚于重置时刻 9:00。
	after := time.Date(2026, 3, 16, 15, 0, 0, 0, tz)
	got := billing.NextFixedWeeklyReset(1, 9, tz, after)
	// 下次重置时间为下周一 9:00。
	want := time.Date(2026, 3, 23, 9, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNextFixedWeeklyReset_TargetDayPast(t *testing.T) {
	tz := time.UTC
	// 当前为 2026-03-18 星期三，每周一重置。
	after := time.Date(2026, 3, 18, 10, 0, 0, 0, tz)
	got := billing.NextFixedWeeklyReset(1, 9, tz, after)
	// 下周一为 2026-03-23。
	want := time.Date(2026, 3, 23, 9, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNextFixedWeeklyReset_Sunday(t *testing.T) {
	tz := time.UTC
	// 当前为 2026-03-14 星期六，每周日重置。
	after := time.Date(2026, 3, 14, 10, 0, 0, 0, tz)
	got := billing.NextFixedWeeklyReset(0, 0, tz, after)
	// 下周日为 2026-03-15。
	want := time.Date(2026, 3, 15, 0, 0, 0, 0, tz)
	assert.Equal(t, want, got)
}

func TestNormalizeFixedQuotaWindows_ClearsExpiredWeeklyWindow(t *testing.T) {
	now := time.Now().UTC()
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	currentWeekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -daysSinceMonday)
	staleStart := currentWeekStart.Add(-24 * time.Hour)
	extra := map[string]any{
		"quota_weekly_limit":      500.0,
		"quota_weekly_used":       76.0,
		"quota_weekly_start":      staleStart.Format(time.RFC3339),
		"quota_weekly_reset_mode": "fixed",
		"quota_weekly_reset_day":  float64(1),
		"quota_weekly_reset_hour": float64(0),
		"quota_reset_timezone":    "UTC",
	}

	NormalizeFixedQuotaWindows(extra, time.Now(), time.LoadLocation)

	assert.Equal(t, 0.0, extra["quota_weekly_used"])
	assert.Equal(t, currentWeekStart.Format(time.RFC3339), extra["quota_weekly_start"])
}

func TestNormalizeFixedQuotaWindows_KeepsActiveWeeklyWindow(t *testing.T) {
	now := time.Now().UTC()
	daysSinceMonday := (int(now.Weekday()) + 6) % 7
	currentWeekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -daysSinceMonday)
	extra := map[string]any{
		"quota_weekly_limit":      500.0,
		"quota_weekly_used":       76.0,
		"quota_weekly_start":      currentWeekStart.Format(time.RFC3339),
		"quota_weekly_reset_mode": "fixed",
		"quota_weekly_reset_day":  float64(1),
		"quota_weekly_reset_hour": float64(0),
		"quota_reset_timezone":    "UTC",
	}

	NormalizeFixedQuotaWindows(extra, time.Now(), time.LoadLocation)

	assert.Equal(t, 76.0, extra["quota_weekly_used"])
	assert.Equal(t, currentWeekStart.Format(time.RFC3339), extra["quota_weekly_start"])
}

func TestComputeQuotaResetAt_RollingMode_NoResetAt(t *testing.T) {
	extra := map[string]any{
		"quota_daily_reset_mode":  "rolling",
		"quota_weekly_reset_mode": "rolling",
	}
	ComputeQuotaResetAt(extra, time.Now(), time.LoadLocation)
	_, hasDailyResetAt := extra["quota_daily_reset_at"]
	_, hasWeeklyResetAt := extra["quota_weekly_reset_at"]
	assert.False(t, hasDailyResetAt, "rolling mode should not set quota_daily_reset_at")
	assert.False(t, hasWeeklyResetAt, "rolling mode should not set quota_weekly_reset_at")
}

func TestComputeQuotaResetAt_RollingMode_ClearsExistingResetAt(t *testing.T) {
	extra := map[string]any{
		"quota_daily_reset_mode":  "rolling",
		"quota_weekly_reset_mode": "rolling",
		"quota_daily_reset_at":    "2026-03-14T09:00:00Z",
		"quota_weekly_reset_at":   "2026-03-16T09:00:00Z",
	}
	ComputeQuotaResetAt(extra, time.Now(), time.LoadLocation)
	_, hasDailyResetAt := extra["quota_daily_reset_at"]
	_, hasWeeklyResetAt := extra["quota_weekly_reset_at"]
	assert.False(t, hasDailyResetAt, "rolling mode should remove quota_daily_reset_at")
	assert.False(t, hasWeeklyResetAt, "rolling mode should remove quota_weekly_reset_at")
}

func TestComputeQuotaResetAt_FixedDaily_SetsResetAt(t *testing.T) {
	extra := map[string]any{
		"quota_daily_reset_mode": "fixed",
		"quota_daily_reset_hour": float64(9),
		"quota_reset_timezone":   "UTC",
	}
	ComputeQuotaResetAt(extra, time.Now(), time.LoadLocation)
	resetAtStr, ok := extra["quota_daily_reset_at"].(string)
	require.True(t, ok, "quota_daily_reset_at should be set")

	resetAt, err := time.Parse(time.RFC3339, resetAtStr)
	require.NoError(t, err)
	// 重置时间应晚于当前时刻。
	assert.True(t, resetAt.After(time.Now()), "reset_at should be in the future")
	// 重置时刻应为 UTC 9:00。
	assert.Equal(t, 9, resetAt.UTC().Hour())
}

func TestComputeQuotaResetAt_FixedWeekly_SetsResetAt(t *testing.T) {
	extra := map[string]any{
		"quota_weekly_reset_mode": "fixed",
		"quota_weekly_reset_day":  float64(1), // Monday
		"quota_weekly_reset_hour": float64(0),
		"quota_reset_timezone":    "UTC",
	}
	ComputeQuotaResetAt(extra, time.Now(), time.LoadLocation)
	resetAtStr, ok := extra["quota_weekly_reset_at"].(string)
	require.True(t, ok, "quota_weekly_reset_at should be set")

	resetAt, err := time.Parse(time.RFC3339, resetAtStr)
	require.NoError(t, err)
	// 重置时间应晚于当前时刻。
	assert.True(t, resetAt.After(time.Now()), "reset_at should be in the future")
	// 重置日期应为周一。
	assert.Equal(t, time.Monday, resetAt.UTC().Weekday())
}

func TestComputeQuotaResetAt_FixedDaily_WithTimezone(t *testing.T) {
	tz, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	extra := map[string]any{
		"quota_daily_reset_mode": "fixed",
		"quota_daily_reset_hour": float64(9),
		"quota_reset_timezone":   "Asia/Shanghai",
	}
	ComputeQuotaResetAt(extra, time.Now(), time.LoadLocation)
	resetAtStr, ok := extra["quota_daily_reset_at"].(string)
	require.True(t, ok)

	resetAt, err := time.Parse(time.RFC3339, resetAtStr)
	require.NoError(t, err)
	// 重置时刻应为上海时间 9:00。
	assert.Equal(t, 9, resetAt.In(tz).Hour())
}

func TestComputeQuotaResetAt_DefaultTimezone(t *testing.T) {
	extra := map[string]any{
		"quota_daily_reset_mode": "fixed",
		"quota_daily_reset_hour": float64(12),
	}
	ComputeQuotaResetAt(extra, time.Now(), time.LoadLocation)
	resetAtStr, ok := extra["quota_daily_reset_at"].(string)
	require.True(t, ok)

	resetAt, err := time.Parse(time.RFC3339, resetAtStr)
	require.NoError(t, err)
	// 默认时区为 UTC。
	assert.Equal(t, 12, resetAt.UTC().Hour())
}

func TestComputeQuotaResetAt_InvalidHour_ClampedToZero(t *testing.T) {
	extra := map[string]any{
		"quota_daily_reset_mode": "fixed",
		"quota_daily_reset_hour": float64(99),
		"quota_reset_timezone":   "UTC",
	}
	ComputeQuotaResetAt(extra, time.Now(), time.LoadLocation)
	resetAtStr, ok := extra["quota_daily_reset_at"].(string)
	require.True(t, ok)

	resetAt, err := time.Parse(time.RFC3339, resetAtStr)
	require.NoError(t, err)
	// 无效小时归零。
	assert.Equal(t, 0, resetAt.UTC().Hour())
}
