package billing_test

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
)

func TestUserSubscriptionNeedsDailyReset_DailyCardKeepsOneTimeQuota(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	dailyWindowStart := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	sub := &billing.UserSubscription{
		StartsAt:         start,
		ExpiresAt:        start.Add(24 * time.Hour),
		DailyWindowStart: &dailyWindowStart,
		DailyUsageUSD:    10,
	}

	require.True(t, sub.HasOneTimeDailyQuota())
	require.False(t, sub.NeedsDailyResetAt(dailyWindowStart.Add(25*time.Hour), timezone.NewCalendar(time.UTC)), "日卡应作为一次性配额，跨 0 点后不再刷新日额度")
}

func TestUserSubscriptionNeedsDailyReset_MultiDaySubscriptionStillRefreshes(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	dailyWindowStart := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	sub := &billing.UserSubscription{
		StartsAt:         start,
		ExpiresAt:        start.AddDate(0, 0, 2),
		DailyWindowStart: &dailyWindowStart,
	}

	require.False(t, sub.HasOneTimeDailyQuota())
	require.True(t, sub.NeedsDailyResetAt(dailyWindowStart.Add(24*time.Hour), timezone.NewCalendar(time.UTC)), "多日订阅仍应在次日零点刷新")
}

func TestUserSubscriptionNeedsDailyReset_LegacyAnchorHealsAtNextMidnight(t *testing.T) {
	base := timezone.NewCalendar(time.UTC).
		StartOfDay(time.Now().UTC())
	legacyWindowStart := base.Add(16*time.Hour + 49*time.Minute)
	sub := &billing.UserSubscription{
		StartsAt:         base.AddDate(0, 0, -3),
		ExpiresAt:        base.AddDate(0, 0, 10),
		DailyWindowStart: &legacyWindowStart,
	}

	require.False(t, sub.NeedsDailyResetAt(base.Add(23*time.Hour+59*time.Minute), timezone.NewCalendar(time.UTC)), "同一日历日内不应重置日额度")
	require.True(t, sub.NeedsDailyResetAt(base.AddDate(0, 0, 1).Add(time.Minute), timezone.NewCalendar(time.UTC)), "旧的非零点锚点应在下一个零点后自愈")
}

func TestUserSubscriptionNeedsDailyReset_SkipsExpiryTailWindow(t *testing.T) {
	start := time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC)
	dailyWindowStart := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 30, 0, 5, 0, 0, time.UTC)
	sub := &billing.UserSubscription{
		StartsAt:         start,
		ExpiresAt:        expiresAt,
		DailyWindowStart: &dailyWindowStart,
	}

	require.False(t, sub.NeedsDailyResetAt(now, timezone.NewCalendar(time.UTC)), "到期当天不足完整日窗口时不应额外刷新 daily usage")
}

func TestUserSubscriptionNeedsDailyReset_ExpiryTailUsesFiniteOuterLimit(t *testing.T) {
	start := time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC)
	dailyWindowStart := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 30, 0, 5, 0, 0, time.UTC)
	dailyLimit := 10.0
	outerLimit := 100.0
	unlimited := 0.0

	tests := []struct {
		name         string
		weeklyLimit  *float64
		monthlyLimit *float64
		want         bool
	}{
		{name: "有限周额度", weeklyLimit: &outerLimit, want: true},
		{name: "有限月额度", monthlyLimit: &outerLimit, want: true},
		{name: "零值周额度", weeklyLimit: &unlimited, want: false},
		{name: "没有外层额度", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := &billing.UserSubscription{
				StartsAt:         start,
				ExpiresAt:        expiresAt,
				DailyWindowStart: &dailyWindowStart,
				DailyLimitUSD:    &dailyLimit,
				WeeklyLimitUSD:   tt.weeklyLimit,
				MonthlyLimitUSD:  tt.monthlyLimit,
			}

			require.Equal(t, tt.want, sub.NeedsDailyResetAt(now, timezone.NewCalendar(time.UTC)))
		})
	}
}

func TestUserSubscriptionNeedsWeeklyReset_ExpiryTailUsesFiniteMonthlyLimit(t *testing.T) {
	start := time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC)
	weeklyWindowStart := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 30, 0, 5, 0, 0, time.UTC)
	weeklyLimit := 50.0
	monthlyLimit := 100.0
	sub := &billing.UserSubscription{
		StartsAt:          start,
		ExpiresAt:         expiresAt,
		WeeklyWindowStart: &weeklyWindowStart,
		WeeklyLimitUSD:    &weeklyLimit,
		MonthlyLimitUSD:   &monthlyLimit,
	}

	require.True(t, sub.NeedsWeeklyResetAt(now), "有限月额度应允许周额度在订阅尾段刷新")
	sub.MonthlyLimitUSD = nil
	require.False(t, sub.NeedsWeeklyResetAt(now), "周额度没有外层保护时仍须容纳完整周窗口")
}

func TestUserSubscriptionNeedsDailyReset_DailyCardIgnoresFiniteOuterLimit(t *testing.T) {
	start := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	dailyWindowStart := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	dailyLimit := 10.0
	monthlyLimit := 100.0
	sub := &billing.UserSubscription{
		StartsAt:         start,
		ExpiresAt:        start.Add(24 * time.Hour),
		DailyWindowStart: &dailyWindowStart,
		DailyLimitUSD:    &dailyLimit,
		MonthlyLimitUSD:  &monthlyLimit,
	}

	require.False(t, sub.NeedsDailyResetAt(time.Date(2026, 5, 31, 0, 5, 0, 0, time.UTC), timezone.NewCalendar(time.UTC)), "1 日卡始终只有一份日额度")
}

func TestUserSubscriptionNeedsMonthlyReset_SkipsExpiryTailWindow(t *testing.T) {
	start := time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC)
	monthlyWindowStart := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 30, 0, 5, 0, 0, time.UTC)
	sub := &billing.UserSubscription{
		StartsAt:           start,
		ExpiresAt:          expiresAt,
		MonthlyWindowStart: &monthlyWindowStart,
	}

	require.False(t, sub.NeedsMonthlyResetAt(now), "到期当天不足完整月窗口时不应额外刷新 monthly usage")
}

func TestUserSubscriptionDailyResetTime_DailyCardReturnsExpiry(t *testing.T) {
	start := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	dailyWindowStart := time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC)
	expiresAt := start.Add(24 * time.Hour)
	sub := &billing.UserSubscription{
		StartsAt:         start,
		ExpiresAt:        expiresAt,
		DailyWindowStart: &dailyWindowStart,
	}

	resetAt := sub.DailyResetTime(timezone.NewCalendar(time.UTC))
	require.NotNil(t, resetAt)
	require.Equal(t, expiresAt, *resetAt, "日卡展示的日额度结束时间应为订阅过期时间")
}

func TestUserSubscriptionDailyResetTime_LegacyAnchorReturnsNextMidnight(t *testing.T) {
	base := timezone.NewCalendar(time.UTC).
		StartOfDay(time.Now().UTC())
	legacyWindowStart := base.Add(16*time.Hour + 49*time.Minute)
	sub := &billing.UserSubscription{
		StartsAt:         base.AddDate(0, 0, -3),
		ExpiresAt:        base.AddDate(0, 0, 10),
		DailyWindowStart: &legacyWindowStart,
	}

	resetAt := sub.DailyResetTime(timezone.NewCalendar(time.UTC))
	require.NotNil(t, resetAt)
	require.Equal(t, base.AddDate(0, 0, 1), *resetAt, "旧的非零点锚点应展示其所在日的下一个零点")
}

func TestUserSubscriptionMonthlyResetTime_TailWindowReturnsExpiry(t *testing.T) {
	start := time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC)
	monthlyWindowStart := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	sub := &billing.UserSubscription{
		StartsAt:           start,
		ExpiresAt:          expiresAt,
		MonthlyWindowStart: &monthlyWindowStart,
	}

	resetAt := sub.MonthlyResetTime()
	require.NotNil(t, resetAt)
	require.Equal(t, expiresAt, *resetAt, "到期尾段展示的月额度结束时间应为订阅过期时间")
}

func TestUserSubscriptionDailyResetTime_TailWindowUsesFiniteMonthlyLimit(t *testing.T) {
	start := time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC)
	dailyWindowStart := time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)
	monthlyLimit := 100.0
	sub := &billing.UserSubscription{
		StartsAt:         start,
		ExpiresAt:        expiresAt,
		DailyWindowStart: &dailyWindowStart,
		MonthlyLimitUSD:  &monthlyLimit,
	}

	resetAt := sub.DailyResetTime(timezone.NewCalendar(time.UTC))
	require.NotNil(t, resetAt)
	require.Equal(t, dailyWindowStart.Add(billing.SubscriptionDailyWindow), *resetAt, "有限月额度保护下应展示尾段日窗口的实际刷新时间")
}

func TestUserSubscriptionWindowActivation_MixedDailyMonthlyTailActivatesOnlyDaily(t *testing.T) {
	now := time.Date(2026, 5, 30, 0, 5, 0, 0, time.UTC)
	dailyLimit := 10.0
	monthlyLimit := 100.0
	sub := &billing.UserSubscription{
		ID:              1,
		Status:          billing.SubscriptionStatusActive,
		StartsAt:        now.AddDate(0, 0, -29),
		ExpiresAt:       now.Add(2 * time.Hour),
		DailyLimitUSD:   &dailyLimit,
		MonthlyLimitUSD: &monthlyLimit,
		MonthlyUsageUSD: 90,
	}

	activation := sub.WindowActivationAt(now)

	require.True(t, activation.Daily, "有限月额度存在时，到期尾段应激活日额度窗口")
	require.False(t, activation.Weekly)
	require.False(t, activation.Monthly, "不足完整月窗口时不应顺带激活月额度窗口")
}

func TestUserSubscriptionWindowActivation_MixedWeeklyMonthlyTailActivatesOnlyWeekly(t *testing.T) {
	now := time.Date(2026, 5, 30, 0, 5, 0, 0, time.UTC)
	weeklyLimit := 50.0
	monthlyLimit := 100.0
	sub := &billing.UserSubscription{
		ID:              1,
		Status:          billing.SubscriptionStatusActive,
		StartsAt:        now.AddDate(0, 0, -29),
		ExpiresAt:       now.Add(2 * time.Hour),
		WeeklyLimitUSD:  &weeklyLimit,
		MonthlyLimitUSD: &monthlyLimit,
		MonthlyUsageUSD: 90,
	}

	activation := sub.WindowActivationAt(now)

	require.False(t, activation.Daily)
	require.True(t, activation.Weekly, "有限月额度存在时，到期尾段应激活周额度窗口")
	require.False(t, activation.Monthly, "月额度没有更高层保护，不应在尾段激活")
}

func TestUserSubscriptionHighestQuotaExhaustedUsesHighestConfiguredWindow(t *testing.T) {
	tests := []struct {
		name       string
		monthly    *float64
		monthlyUse float64
		weekly     *float64
		weeklyUse  float64
		daily      *float64
		dailyUse   float64
		want       bool
	}{
		{name: "monthly exhausted", monthly: quotaPointer(100), monthlyUse: 100, weekly: quotaPointer(10), weeklyUse: 10, daily: quotaPointer(1), dailyUse: 1, want: true},
		{name: "monthly available blocks lower exhausted windows", monthly: quotaPointer(100), monthlyUse: 99, weekly: quotaPointer(10), weeklyUse: 10, daily: quotaPointer(1), dailyUse: 1, want: false},
		{name: "weekly exhausted when monthly absent", weekly: quotaPointer(10), weeklyUse: 10, daily: quotaPointer(1), dailyUse: 1, want: true},
		{name: "weekly available blocks daily exhausted", weekly: quotaPointer(10), weeklyUse: 9, daily: quotaPointer(1), dailyUse: 1, want: false},
		{name: "daily exhausted when it is the only limit", daily: quotaPointer(1), dailyUse: 1, want: true},
		{name: "unlimited has no revocable quota", daily: nil, want: false},
		{name: "non-finite quota is not revocable", monthly: quotaPointer(math.Inf(1)), monthlyUse: math.Inf(1), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := revokeSubscriptionFixture()
			sub.MonthlyLimitUSD, sub.MonthlyUsageUSD = tt.monthly, tt.monthlyUse
			sub.WeeklyLimitUSD, sub.WeeklyUsageUSD = tt.weekly, tt.weeklyUse
			sub.DailyLimitUSD, sub.DailyUsageUSD = tt.daily, tt.dailyUse
			require.Equal(t, tt.want, sub.HighestQuotaExhausted())
		})
	}
}
