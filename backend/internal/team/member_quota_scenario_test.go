package team_test

// 本场景覆盖 team.go、internal/apikey/service.go 和 internal/billing/member_quota.go。
// 团队成员额度快照经 KeyCheckTeamMemberLimitSnapshot 交给 Billing 校验。

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/team"
)

func TestCheckTeamMemberLimitSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		member *team.TeamMembership
		want   error
	}{
		{name: "unlimited", member: &team.TeamMembership{Role: team.TeamRoleMember}},
		{name: "owner_ignores_limits", member: &team.TeamMembership{Role: team.TeamRoleOwner, DailyLimitUSD: 1, DailyUsageUSD: 1}},
		{name: "daily", member: &team.TeamMembership{Role: team.TeamRoleMember, DailyLimitUSD: 1, DailyUsageUSD: 1}, want: team.ErrTeamMemberDailyExceeded},
		{name: "weekly", member: &team.TeamMembership{Role: team.TeamRoleMember, WeeklyLimitUSD: 2, WeeklyUsageUSD: 3}, want: team.ErrTeamMemberWeeklyExceeded},
		{name: "monthly", member: &team.TeamMembership{Role: team.TeamRoleMember, MonthlyLimitUSD: 4, MonthlyUsageUSD: 4}, want: team.ErrTeamMemberMonthlyExceeded},
		{name: "below_limits", member: &team.TeamMembership{Role: team.TeamRoleMember, DailyLimitUSD: 2, DailyUsageUSD: 1, WeeklyLimitUSD: 5, WeeklyUsageUSD: 4, MonthlyLimitUSD: 10, MonthlyUsageUSD: 9}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := apikey.KeyCheckTeamMemberLimitSnapshot(tt.member)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}
