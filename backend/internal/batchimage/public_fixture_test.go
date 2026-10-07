package batchimage_test

import (
	"context"

	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	batchprovider "github.com/TokenFlux/TokenRouter/internal/batchimage/provider"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/gateway/completion"
	"github.com/TokenFlux/TokenRouter/internal/gateway/modeltrace"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// batchProvidersFixtureSource 提供候选查询数据，候选筛选与资金处理由生产模块执行。
type batchProvidersFixtureSource interface {
	GetByID(context.Context, int64) (*providercore.Record, error)
	ListSchedulableByPlatform(context.Context, string) ([]providercore.Record, error)
	ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]providercore.Record, error)
}
type batchGroupFixtureSource interface {
	GetByIDLite(context.Context, int64) (*batchimage.GroupView, error)
}

type batchProviderFixture struct {
	source   batchProvidersFixtureSource
	registry *batchimage.Registry[batchprovider.BatchImageProvider]
}

func (r *batchProviderFixture) project(value *providercore.Record) *batchimage.Candidate {
	return (&batchprovider.Candidates{Registry: r.registry, ObserveModel: modeltrace.RegisterStage}).Project(providercore.CloneRecord(value))
}

func (r *batchProviderFixture) GetByID(ctx context.Context, id int64) (*batchimage.Candidate, error) {
	v, err := r.source.GetByID(ctx, id)
	return r.project(v), err
}

func (r *batchProviderFixture) values(rows []providercore.Record) []batchimage.Candidate {
	out := make([]batchimage.Candidate, len(rows))
	for i := range rows {
		out[i] = *r.project(&rows[i])
	}
	return out
}

func (r *batchProviderFixture) ListSchedulableByPlatform(ctx context.Context, p string) ([]batchimage.Candidate, error) {
	v, err := r.source.ListSchedulableByPlatform(ctx, p)
	return r.values(v), err
}

func (r *batchProviderFixture) ListSchedulableByGroupIDAndPlatform(ctx context.Context, id int64, p string) ([]batchimage.Candidate, error) {
	v, err := r.source.ListSchedulableByGroupIDAndPlatform(ctx, id, p)
	return r.values(v), err
}

func rebindBatchFixtureProviders(core *batchimage.Public, source batchProvidersFixtureSource) batchimage.ProviderReader {
	return &batchProviderFixture{source: source, registry: testassert.MustType[*batchProviderFixture](core.ProviderRepo).registry}
}

type batchGroupReader struct{ source batchGroupFixtureSource }

func (r batchGroupReader) GetByIDLite(ctx context.Context, id int64) (*batchimage.GroupView, error) {
	v, err := r.source.GetByIDLite(ctx, id)
	return batchGroupProjection(v), err
}

func batchGroupProjection(v *batchimage.GroupView) *batchimage.GroupView { return v }

func taskFixtureBilling(core *batchimage.Public) batchimage.FundingStore { return core.Funding.Store }

// newBatchPublicFixture 只构造端口与选项；测试修改的资金替身仍在调用时读取。
func newBatchPublicFixture(repo batchimage.BatchImageRepository, providers batchProvidersFixtureSource, pricingConfigs *routing.PricingConfigService, groups batchGroupFixtureSource, rates batchimage.BatchImageUserGroupRateRepository, queue batchimage.BatchImageQueue, registry *batchimage.Registry[batchprovider.BatchImageProvider], prices batchimage.ImagePricer, funds batchimage.FundingStore, auth apikey.APIKeyAuthCacheInvalidator, cfg *config.Config) *batchimage.Public {
	core := &batchimage.Public{Repo: repo, UserGroupRateRepo: rates, Queue: queue, Pricing: prices, Funding: nativeTaskFundingFixture(funds), Observe: resultObserve}
	if providers != nil {
		core.ProviderRepo = &batchProviderFixture{source: providers, registry: registry}
	}
	if groups == nil {
		groups = &publicBatchImageGroupRepo{groups: map[int64]*batchimage.GroupView{7: {ID: 7, AllowBatchImageGeneration: true, RateMultiplier: 1, BatchImageDiscountMultiplier: 0.5, BatchImageHoldMultiplier: 0.6}}}
	}
	core.GroupRepo = batchGroupReader{groups}
	if pricingConfigs != nil {
		core.PricingConfigService = pricingConfigs
	}
	if cfg != nil {
		c := cfg.BatchImage
		core.Options = batchimage.PublicOptions{Enabled: c.Enabled, StaleActiveAfterSeconds: c.StaleActiveAfterSeconds, MaxItemsPerJobDefault: c.MaxItemsPerJobDefault, MaxOutputImagesPerJob: c.MaxOutputImagesPerJob, MaxOutputImagesPerItem: c.MaxOutputImagesPerItem, MaxPromptCharsPerItem: c.MaxPromptCharsPerItem, MaxReferenceImagesPerJob: c.MaxReferenceImagesPerJob, MaxReferenceInlineBytesPerJob: c.MaxReferenceInlineBytesPerJob, DefaultResponseMimeType: c.DefaultResponseMimeType, DefaultImageSize: c.DefaultImageSize}
	}
	core.ProviderExists = func(name string) bool {
		value, ok := registry.Get(name)
		return ok && value != nil
	}
	if auth != nil {
		core.InvalidateAuth = auth.InvalidateAuthCacheByUserID
	}
	core.ClientModel = func(ctx context.Context) string {
		v, _ := ctx.Value(telemetry.ClientModel).(string)
		return v
	}
	core.WithModelTrace = func(ctx context.Context, m routing.GroupMappingResult, requested string) routing.GroupMappingResult {
		return modeltrace.WithGroupRedirect(m, ctx, requested)
	}
	core.RegisterModel = modeltrace.RegisterStage
	core.AutoSubscription = func(ctx context.Context, userID int64, groupID *int64) *billing.UserSubscription {
		reader, _ := taskFixtureBilling(core).(completion.SubscriptionReader)
		return completion.ResolveSubscription(ctx, nil, reader, userID, groupID)
	}
	core.PreferredSubscription = func(ctx context.Context, userID, id int64, groupID *int64) *billing.UserSubscription {
		reader, _ := taskFixtureBilling(core).(billing.PreferredSubscriptionReader)
		return billing.ResolvePreferredSubscription(ctx, reader, userID, id, groupID)
	}
	core.SubscriptionMultiplier = func(ctx context.Context, owner batchimage.BatchImageOwner, group *batchimage.GroupView, fallback float64, sub *billing.UserSubscription) float64 {
		var projected *completion.GroupSnapshot
		if group != nil {
			projected = &completion.GroupSnapshot{ID: group.ID, RateMultiplier: group.RateMultiplier}
		}
		return completion.ResolveUsageRateMultiplier(ctx, owner.EffectiveBillingUserID(), owner.GroupID, projected, fallback, sub, nil)
	}
	return core
}
