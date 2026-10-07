package routing_test

import (
	"context"
	"log/slog"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"

	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"

	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	routingprovider "github.com/TokenFlux/TokenRouter/internal/routing/provider"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
)

// groupAdminPricingStore 为目录测试提供空价卡，模型权限只由分组配置决定。
type groupAdminPricingStore struct {
	routing.PricingConfigRepository
}

func (groupAdminPricingStore) ListAll(context.Context) ([]routing.PricingConfig, error) {
	return nil, nil
}

// newGroupAdminForTest 使用独立存储副本及默认策略构造分组管理用例。
func newGroupAdminForTest(repo routing.GroupRepository, duplicate routing.GroupDuplicateRepository, pricingConfigs routing.GroupPricingInvalidator) *routing.GroupAdmin {
	return newGroupAdminPortsForTest(repo, duplicate, pricingConfigs, nil, nil, nil, nil)
}

// newGroupAdminPortsForTest 装配分组管理测试所需的端口。
func newGroupAdminPortsForTest(repo routing.GroupRepository, duplicate routing.GroupDuplicateRepository, pricingConfigs routing.GroupPricingInvalidator, sortOrder routing.GroupSortOrderRepository, providers routing.GroupProviders, invalidator routing.GroupAdminInvalidator, weights *policy.ConfigScoreWeights, keyReaders ...routing.GroupKeyReader) *routing.GroupAdmin {
	var keys routing.GroupKeyReader
	if len(keyReaders) > 0 {
		keys = keyReaders[0]
	}
	var duplicates routing.GroupDuplicateRepository
	if duplicate != nil {
		duplicates = groupDuplicatePortFixture{duplicate}
	}
	modelPolicies := routing.NewPricingConfigService(groupAdminPricingStore{}, nil, routing.PricingConfigOptions{ReadGroup: func(ctx context.Context, id int64) (*routing.Group, error) {
		group, err := repo.GetByIDLite(ctx, id)
		if group != nil {
			group = routing.CloneGroup(group)
			if group.AllowedProtocols == nil {
				group.AllowedProtocols = capability.DefaultGroupClientProtocols("")
			}
		}
		return group, err
	}})
	return routing.NewGroupAdmin(groupPortFixture{repo}, duplicates, sortOrder, providers, keys, invalidator, pricingConfigs, routing.GroupAdminOptions{
		DefaultModels: routingprovider.DefaultGroupModelCandidates,
		ModelResolver: routing.RequestableResolver{
			GroupPolicies: modelPolicies,
			Defaults:      gatewayprovider.CatalogueDefaults(),
			Warn:          slog.Warn,
		},
		GlobalWeights: func(ctx context.Context) (policy.ScoreWeights, error) {
			defaults := scheduler.DefaultAdminSettingsDefaults()
			if weights != nil {
				defaults.Weights = *weights
			}
			return scheduler.LoadValidationWeights(ctx, nil, defaults)
		},
		Mutate: func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) },
	})
}

type groupPortFixture struct{ routing.GroupRepository }

func testGroupsFixture(values []routing.Group) []routing.Group {
	if values == nil {
		return nil
	}
	out := make([]routing.Group, len(values))
	for i := range values {
		out[i] = *routing.CloneGroup(&values[i])
	}
	return out
}

func (r groupPortFixture) Create(ctx context.Context, value *routing.Group) error {
	old := routing.CloneGroup(value)
	err := r.GroupRepository.Create(ctx, old)
	*value = *routing.CloneGroup(old)
	return err
}

func (r groupPortFixture) Update(ctx context.Context, value *routing.Group) error {
	old := routing.CloneGroup(value)
	err := r.GroupRepository.Update(ctx, old)
	*value = *routing.CloneGroup(old)
	return err
}

func (r groupPortFixture) GetByID(ctx context.Context, id int64) (*routing.Group, error) {
	value, err := r.GroupRepository.GetByID(ctx, id)
	return routing.CloneGroup(value), err
}

func (r groupPortFixture) GetByIDLite(ctx context.Context, id int64) (*routing.Group, error) {
	value, err := r.GroupRepository.GetByIDLite(ctx, id)
	return routing.CloneGroup(value), err
}

func (r groupPortFixture) List(ctx context.Context, params pagination.PaginationParams) ([]routing.Group, *pagination.PaginationResult, error) {
	v, p, e := r.GroupRepository.List(ctx, params)
	return testGroupsFixture(v), p, e
}

func (r groupPortFixture) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]routing.Group, *pagination.PaginationResult, error) {
	v, p, e := r.GroupRepository.ListWithFilters(ctx, params, platform, status, search, isExclusive)
	return testGroupsFixture(v), p, e
}

func (r groupPortFixture) ListActive(ctx context.Context) ([]routing.Group, error) {
	v, e := r.GroupRepository.ListActive(ctx)
	return testGroupsFixture(v), e
}

type groupDuplicatePortFixture struct {
	routing.GroupDuplicateRepository
}

func (p groupDuplicatePortFixture) FindByDuplicateOperationID(ctx context.Context, id string) (*routing.Group, error) {
	value, err := p.GroupDuplicateRepository.FindByDuplicateOperationID(ctx, id)
	return routing.CloneGroup(value), err
}

func (p groupDuplicatePortFixture) CreateFromSource(ctx context.Context, value *routing.Group, id int64) error {
	copy := routing.CloneGroup(value)
	err := p.GroupDuplicateRepository.CreateFromSource(ctx, copy, id)
	*value = *routing.CloneGroup(copy)
	return err
}
