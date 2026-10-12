package admission

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

// FundingCheck 根据计费检查输入验证权益。
type FundingCheck interface {
	Check(context.Context, billing.CheckInput) error
}

// RPMCheck 由调度模块拥有计数与覆盖规则。
type RPMCheck interface {
	Check(context.Context, *scheduler.RPMUser, *scheduler.RPMGroup) error
}

// FundingAdmission 固定绑定两个用例，保持资金错误先于 RPM 消费。
type FundingAdmission struct {
	funds FundingCheck
	rpm   RPMCheck
}

// NewFundingAdmission 固定资金与 RPM 检查依赖。
func NewFundingAdmission(funds FundingCheck, rpm RPMCheck) *FundingAdmission {
	return &FundingAdmission{funds: funds, rpm: rpm}
}

// Check 首先检查资金，通过后执行 RPM；不在资金拒绝时累计次数。
func (a *FundingAdmission) Check(ctx context.Context, input billing.CheckInput, user *scheduler.RPMUser, group *scheduler.RPMGroup) error {
	if err := a.CheckFunding(ctx, input); err != nil {
		return err
	}
	if a.rpm == nil || user == nil {
		return nil
	}
	return a.rpm.Check(ctx, user, group)
}

// CheckFunding 在等待结束后复查权益，RPM 已在进入等待前累计。
func (a *FundingAdmission) CheckFunding(ctx context.Context, input billing.CheckInput) error {
	return a.funds.Check(ctx, input)
}

// CheckKey 从已认证并完成最终分组授权的 Key 提取资金和 RPM 检查字段。
func (a *FundingAdmission) CheckKey(ctx context.Context, key *apikey.APIKey, subscription *billing.UserSubscription, platform string, afterWait bool) error {
	var payer *billing.UserSummary
	var user *scheduler.RPMUser
	if key.User != nil {
		payer = &billing.UserSummary{ID: key.User.ID}
		user = &scheduler.RPMUser{ID: key.User.ID, RPMLimit: key.User.RPMLimit, UserGroupRPMOverride: key.User.UserGroupRPMOverride}
	}
	var group *billing.GroupSnapshot
	var rpmGroup *scheduler.RPMGroup
	if key.Group != nil {
		group = &billing.GroupSnapshot{ID: key.Group.ID}
		rpmGroup = &scheduler.RPMGroup{ID: key.Group.ID, RPMLimit: key.Group.RPMLimit}
	}
	input := billing.CheckInput{
		Payer: payer,
		Key:   &billing.KeySnapshot{ID: key.ID, BillingMode: key.BillingMode, RateLimit5h: key.RateLimit5h, RateLimit1d: key.RateLimit1d, RateLimit7d: key.RateLimit7d},
		Group: group, Subscription: subscription, Platform: platform,
	}
	if afterWait {
		return a.CheckFunding(ctx, input)
	}
	return a.Check(ctx, input, user, rpmGroup)
}
