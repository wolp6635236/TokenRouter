package app

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// keyGroups 从 routing 读取 Key 需要的分组数据。
type keyGroups struct{ Repository routing.GroupRepository }

func (p keyGroups) GetByID(ctx context.Context, id int64) (*routing.Group, error) {
	v, e := p.Repository.GetByID(ctx, id)
	return apikey.GroupFromRouting(v), e
}

func (p keyGroups) GetByIDLite(ctx context.Context, id int64) (*routing.Group, error) {
	v, e := p.Repository.GetByIDLite(ctx, id)
	return apikey.GroupFromRouting(v), e
}

func (p keyGroups) ListActive(ctx context.Context) ([]routing.Group, error) {
	v, e := p.Repository.ListActive(ctx)
	if v == nil {
		return nil, e
	}
	out := make([]routing.Group, len(v))
	for i := range v {
		out[i] = *apikey.GroupFromRouting(&v[i])
	}
	return out, e
}

func keyGroupFastPolicy(raw string, force bool) string {
	return (&routing.Group{OpenAIFastPolicy: raw, ForceOpenAIFast: force}).EffectiveOpenAIFastPolicy()
}

// identityAdminGroups 从 routing 读取用户管理需要的分组字段。
type identityAdminGroups struct{ Repository routing.GroupRepository }

func (p identityAdminGroups) GetByID(ctx context.Context, id int64) (*identity.AdminGroup, error) {
	v, e := p.Repository.GetByID(ctx, id)
	return identityAdminGroup(v), e
}

func (p identityAdminGroups) GetByIDLite(ctx context.Context, id int64) (*identity.AdminGroup, error) {
	v, e := p.Repository.GetByIDLite(ctx, id)
	return identityAdminGroup(v), e
}

func identityAdminGroup(v *routing.Group) *identity.AdminGroup {
	if v == nil {
		return nil
	}
	return &identity.AdminGroup{ID: v.ID, Name: v.Name, Status: v.Status, IsExclusive: v.IsExclusive, RPMLimit: v.RPMLimit}
}

// billingGroups 从 routing 读取套餐展示需要的分组名称。
type billingGroups struct{ Repository routing.GroupRepository }

func (b billingGroups) GetByIDLite(ctx context.Context, id int64) (*billing.SubscriptionPlanGroup, error) {
	if b.Repository == nil {
		return nil, nil
	}
	g, err := b.Repository.GetByIDLite(ctx, id)
	if err != nil || g == nil {
		return nil, err
	}
	// 订阅接口展示请求语言的分组文案。
	display, _ := routing.GroupDisplay(g, locale.FromContext(ctx))
	return &billing.SubscriptionPlanGroup{ID: g.ID, Name: display.DisplayName}, nil
}
