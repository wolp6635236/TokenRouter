package billing

import (
	"time"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"
)

var (
	ErrTeamMemberDailyExceeded   = apperror.TooManyRequests("TEAM_MEMBER_DAILY_LIMIT_EXCEEDED", "The team member daily limit has been reached")
	ErrTeamMemberMonthlyExceeded = apperror.TooManyRequests("TEAM_MEMBER_MONTHLY_LIMIT_EXCEEDED", "The team member monthly limit has been reached")
	ErrTeamMemberWeeklyExceeded  = apperror.TooManyRequests("TEAM_MEMBER_WEEKLY_LIMIT_EXCEEDED", "The team member weekly limit has been reached")
	ErrTeamMembershipRequired    = apperror.Forbidden("TEAM_MEMBERSHIP_REQUIRED", "Team membership is required")
)

// MemberQuotaSnapshot 只描述成员资金窗口，团队角色和是否检查由调用用例决定。
type MemberQuotaSnapshot struct {
	DailyLimitUSD, WeeklyLimitUSD, MonthlyLimitUSD          float64
	DailyUsageUSD, WeeklyUsageUSD, MonthlyUsageUSD          float64
	DailyWindowStart, WeeklyWindowStart, MonthlyWindowStart *time.Time
}

// CheckMemberQuotaSnapshot 保留日、周、月顺序，零限额不限制，不在预检中改写窗口。
func CheckMemberQuotaSnapshot(member MemberQuotaSnapshot) error {
	if member.DailyLimitUSD > 0 && member.DailyUsageUSD >= member.DailyLimitUSD {
		return ErrTeamMemberDailyExceeded
	}
	if member.WeeklyLimitUSD > 0 && member.WeeklyUsageUSD >= member.WeeklyLimitUSD {
		return ErrTeamMemberWeeklyExceeded
	}
	if member.MonthlyLimitUSD > 0 && member.MonthlyUsageUSD >= member.MonthlyLimitUSD {
		return ErrTeamMemberMonthlyExceeded
	}
	return nil
}

// NormalizeMemberQuotaWindows 返回按当前日期重置过期窗口的额度副本，持久化消费和重置由事务执行。
func NormalizeMemberQuotaWindows(member MemberQuotaSnapshot, now time.Time, calendar timezone.Calendar) MemberQuotaSnapshot {
	daily, weekly, monthly := calendar.StartOfDay(now), calendar.StartOfWeek(now), calendar.StartOfMonth(now)
	if member.DailyWindowStart == nil || member.DailyWindowStart.Before(daily) {
		member.DailyUsageUSD = 0
		member.DailyWindowStart = &daily
	}
	if member.WeeklyWindowStart == nil || member.WeeklyWindowStart.Before(weekly) {
		member.WeeklyUsageUSD = 0
		member.WeeklyWindowStart = &weekly
	}
	if member.MonthlyWindowStart == nil || member.MonthlyWindowStart.Before(monthly) {
		member.MonthlyUsageUSD = 0
		member.MonthlyWindowStart = &monthly
	}
	return member
}
