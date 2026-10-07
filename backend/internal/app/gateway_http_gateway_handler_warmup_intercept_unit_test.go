package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	authctx "github.com/TokenFlux/TokenRouter/internal/identity/httpapi/authctx"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	identity "github.com/TokenFlux/TokenRouter/internal/identity"
	logging "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"

	"github.com/TokenFlux/TokenRouter/internal/scheduler/rediscache/codec"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Antigravity 提供商通过 /v1/messages 提供 Claude 服务时，
// 当提供商 credentials.intercept_warmup_requests=true 且请求为 Warmup 时，
// 后端在转发上游前拦截请求并返回 mock 响应。

type fakeSchedulerCache struct {
	providers []*gatewayprovider.ExecutionProvider
}

func (f *fakeSchedulerCache) GetSnapshot(_ context.Context, _ scheduler.SchedulerBucket) ([]scheduler.SnapshotProvider, bool, error) {
	if f.providers == nil {
		return nil, true, nil
	}
	values := make([]scheduler.SnapshotProvider, len(f.providers))
	for i, value := range f.providers {
		values[i] = codec.WrapRecord(gatewayprovider.ExecutionRecord(value))
	}
	return values, true, nil
}

func (f *fakeSchedulerCache) CaptureBucketWriteToken(_ context.Context, bucket scheduler.SchedulerBucket) (scheduler.SchedulerBucketWriteToken, error) {
	return scheduler.SchedulerBucketWriteToken{Bucket: bucket, Epoch: 1}, nil
}

func (f *fakeSchedulerCache) SetSnapshot(_ context.Context, _ scheduler.SchedulerBucket, _ scheduler.SchedulerBucketWriteToken, _ []scheduler.SnapshotProvider) error {
	return nil
}

func (f *fakeSchedulerCache) RetireBucket(_ context.Context, _ scheduler.SchedulerBucket) error {
	return nil
}

func (f *fakeSchedulerCache) ReopenBucket(_ context.Context, bucket scheduler.SchedulerBucket) (scheduler.SchedulerBucketWriteToken, error) {
	return scheduler.SchedulerBucketWriteToken{Bucket: bucket, Epoch: 1}, nil
}

func (f *fakeSchedulerCache) TryAcquireGroupLifecycleLease(_ context.Context, _ int64, _ time.Duration) (scheduler.SchedulerGroupLifecycleLease, bool, error) {
	return scheduler.SchedulerGroupLifecycleLease{}, false, nil
}

func (f *fakeSchedulerCache) ReleaseGroupLifecycleLease(_ context.Context, _ scheduler.SchedulerGroupLifecycleLease) error {
	return nil
}

func (f *fakeSchedulerCache) GetProvider(_ context.Context, id int64) (scheduler.SnapshotProvider, error) {
	for _, provider := range f.providers {
		if provider != nil && provider.Record.ID == id {
			return codec.WrapRecord(gatewayprovider.ExecutionRecord(provider)), nil
		}
	}
	return nil, nil
}

func (f *fakeSchedulerCache) SetProvider(_ context.Context, _ scheduler.SnapshotProvider) error {
	return nil
}
func (f *fakeSchedulerCache) DeleteProvider(_ context.Context, _ int64) error { return nil }
func (f *fakeSchedulerCache) UpdateLastUsed(_ context.Context, _ map[int64]time.Time) error {
	return nil
}

func (f *fakeSchedulerCache) TryLockBucket(_ context.Context, _ scheduler.SchedulerBucket, _ time.Duration) (bool, error) {
	return true, nil
}

func (f *fakeSchedulerCache) UnlockBucket(_ context.Context, _ scheduler.SchedulerBucket) error {
	return nil
}

func (f *fakeSchedulerCache) ListBuckets(_ context.Context) ([]scheduler.SchedulerBucket, error) {
	return nil, nil
}
func (f *fakeSchedulerCache) GetOutboxWatermark(_ context.Context) (int64, error) { return 0, nil }
func (f *fakeSchedulerCache) SetOutboxWatermark(_ context.Context, _ int64) error { return nil }

type fakeGroupRepo struct {
	group *routing.Group
}

func (f *fakeGroupRepo) Create(context.Context, *routing.Group) error { return nil }
func (f *fakeGroupRepo) GetByID(context.Context, int64) (*routing.Group, error) {
	return f.group, nil
}

func (f *fakeGroupRepo) GetByIDLite(context.Context, int64) (*routing.Group, error) {
	return f.group, nil
}
func (f *fakeGroupRepo) Update(context.Context, *routing.Group) error          { return nil }
func (f *fakeGroupRepo) Delete(context.Context, int64) error                   { return nil }
func (f *fakeGroupRepo) DeleteCascade(context.Context, int64) ([]int64, error) { return nil, nil }
func (f *fakeGroupRepo) List(context.Context, pagination.PaginationParams) ([]routing.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (f *fakeGroupRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]routing.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (f *fakeGroupRepo) ListActive(context.Context) ([]routing.Group, error) { return nil, nil }
func (f *fakeGroupRepo) ListActiveByPlatform(context.Context, string) ([]routing.Group, error) {
	return nil, nil
}

func (f *fakeGroupRepo) ListActiveByPlatformLite(ctx context.Context, platform string) ([]routing.Group, error) {
	return f.ListActiveByPlatform(ctx, platform)
}
func (f *fakeGroupRepo) ExistsByName(context.Context, string) (bool, error) { return false, nil }
func (f *fakeGroupRepo) GetProviderCount(context.Context, int64) (int64, int64, error) {
	return 0, 0, nil
}

func (f *fakeGroupRepo) DeleteProviderGroupsByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}

func (f *fakeGroupRepo) GetProviderIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return nil, nil
}
func (f *fakeGroupRepo) BindProvidersToGroup(context.Context, int64, []int64) error { return nil }
func (f *fakeGroupRepo) UpdateSortOrders(context.Context, []routing.GroupSortOrderUpdate) error {
	return nil
}

type fakeConcurrencyCache struct{}

func (f *fakeConcurrencyCache) AcquireProviderSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (f *fakeConcurrencyCache) ReleaseProviderSlot(context.Context, int64, string) error { return nil }

func (f *fakeConcurrencyCache) GetProviderConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}

func (f *fakeConcurrencyCache) IncrementProviderWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}

func (f *fakeConcurrencyCache) DecrementProviderWaitCount(context.Context, int64) error { return nil }

func (f *fakeConcurrencyCache) GetProviderWaitingCount(context.Context, int64) (int, error) {
	return 0, nil
}

func (f *fakeConcurrencyCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (f *fakeConcurrencyCache) ReleaseUserSlot(context.Context, int64, string) error { return nil }

func (f *fakeConcurrencyCache) GetUserConcurrency(context.Context, int64) (int, error) { return 0, nil }

func (f *fakeConcurrencyCache) IncrementWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (f *fakeConcurrencyCache) DecrementWaitCount(context.Context, int64) error { return nil }
func (f *fakeConcurrencyCache) GetProvidersLoadBatch(context.Context, []scheduler.ProviderWithConcurrency) (map[int64]*scheduler.ProviderLoadInfo, error) {
	return map[int64]*scheduler.ProviderLoadInfo{}, nil
}

func (f *fakeConcurrencyCache) GetUsersLoadBatch(context.Context, []scheduler.UserWithConcurrency) (map[int64]*scheduler.UserLoadInfo, error) {
	return map[int64]*scheduler.UserLoadInfo{}, nil
}

func (f *fakeConcurrencyCache) GetProviderConcurrencyBatch(_ context.Context, providerIDs []int64) (map[int64]int, error) {
	result := make(map[int64]int, len(providerIDs))
	for _, id := range providerIDs {
		result[id] = 0
	}
	return result, nil
}

func (f *fakeConcurrencyCache) CleanupExpiredProviderSlots(context.Context, int64) error { return nil }

func (f *fakeConcurrencyCache) CleanupExpiredProviderSlotKeys(context.Context) error { return nil }

func (f *fakeConcurrencyCache) CleanupStaleProcessSlots(context.Context, string) error { return nil }

func newTestGatewayHandler(t *testing.T, group *routing.Group, providers []*gatewayprovider.ExecutionProvider) (*messageEndpointsFixture, func()) {
	t.Helper()

	schedulerCache := &fakeSchedulerCache{providers: providers}
	schedulerSnapshot := scheduler.NewSnapshotService(schedulerCache, nil, nil, nil, nil, scheduler.SnapshotBindings{})

	gwSvc, gwSvcChoices, messages := newGenericExecutionAndSelectionFixture(
		nil,                               // providerRepo (not used: scheduler snapshot hit)
		&fakeGroupRepo{group: group}, nil, // usageLogRepo
		// usageBillingRepo
		// userRepo
		// userSubRepo
		// userGroupRateRepo
		nil, // cache (disable sticky)
		nil, // cfg
		schedulerSnapshot,
		nil, // concurrencyService (disable load-aware; tryAcquire always acquired)
		// billingService
		nil, // healthObserver
		// billingCacheService
		nil,      // identityService
		nil, nil, // httpUpstream
		// deferredService
		nil,      // claudeTokenProvider
		nil, nil, // sessionLimitCache
		nil, // rpmCache
		nil, // digestStore
		nil, // settingService
		nil, // tlsFPProfileService
		nil, // channelService
		nil, // resolver
		// balanceNotifyService
		responseHeaderFilterForTest(nil),
	)
	// 预热用例直接绑定完成器。
	gwSvc.Recorder = newHTTPCompletionFixture(nil, nil, nil, nil, nil, nil, nil, false)

	cfg := &config.Config{}
	billingCacheSvc := newBillingEligibilityFixture(cfg)
	billingCacheSvc.Start()

	concurrencySvc := scheduler.NewConcurrencyService(&fakeConcurrencyCache{}, scheduler.Diagnostics{
		Logf:  logging.LegacyPrintf,
		Event: logging.Event,
	},
	)
	concurrencyHelper := gatewayhttp.NewConcurrencyHelper(concurrencySvc, gatewayhttp.SSEPingFormatClaude, 0)

	h := newMessageEndpointsFixture(gwSvc, messages, newFundingAdmissionFixture(billingCacheSvc, cfg), concurrencyHelper, gatewayhttp.MessagesHTTPOptions{MaxBodyBytes: openAITextOptions(nil).MaxBodyBytes, MaxSwitches: 1, MaxGeminiSwitches: 1}, newExecutionAvailabilityForTest(nil,

		nil, nil), gwSvcChoices,
	)

	cleanup := func() {
		billingCacheSvc.Stop()
	}
	return h, cleanup
}

func TestGatewayHandlerMessages_InterceptWarmup_AntigravityProvider_MixedSchedulingV1(t *testing.T) {
	groupID := int64(2001)
	providerID := int64(1001)

	group := &routing.Group{
		ID:       groupID,
		Hydrated: true,
		// /v1/messages（Claude兼容）入口
		Status: billing.StatusActive,
	}

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: providerID,
			Name:     "ag-1",
			Platform: capability.PlatformAntigravity,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"access_token":              "tok_xxx",
				"intercept_warmup_requests": true,
			},
			Extra: map[string]any{
				"mixed_scheduling": true, // 关键：允许被 anthropic 分组混合调度选中
			},
			Concurrency:    1,
			Priority:       1,
			Status:         billing.StatusActive,
			Schedulable:    true,
			ProviderGroups: []providercore.GroupMembership{{ProviderID: providerID, GroupID: groupID}},
		},
	}

	h, cleanup := newTestGatewayHandler(t, group, []*gatewayprovider.ExecutionProvider{provider})
	defer cleanup()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body := []byte(`{
		"model": "claude-sonnet-4-5",
		"max_tokens": 256,
		"messages": [{"role":"user","content":[{"type":"text","text":"Warmup"}]}]
	}`)
	req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(requeststate.WithGroup(req.Context(), group))
	c.Request = req

	apiKey := &apikey.APIKey{
		ID:      3001,
		UserID:  4001,
		GroupID: &groupID,
		Status:  billing.StatusActive,
		User: &identity.User{
			ID:          4001,
			Concurrency: 10,
			Balance:     100,
		},
		Group: group,
	}

	c.Set(string(keyhttp.ContextKeyAPIKey), apiKey)
	c.Set(string(authctx.ContextKeyUser), authctx.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, 200, rec.Code)

	// 检查 Handler 选中的提供商为 antigravity。
	selected, ok := c.Get(gatewayhttp.OpsProviderIDKey)
	require.True(t, ok)
	require.Equal(t, providerID, selected)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Regexp(t, `^msg_01[0-9A-Za-z]{22}$`, resp["id"])
	require.Equal(t, "claude-sonnet-4-5", resp["model"])

	content, ok := resp["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	first, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "New Conversation", first["text"])
}

func TestGatewayHandlerMessages_InterceptWarmup_AntigravityProvider_ForcePlatform(t *testing.T) {
	groupID := int64(2002)
	providerID := int64(1002)

	group := &routing.Group{
		ID:       groupID,
		Hydrated: true,
		Status:   billing.StatusActive,
	}

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: providerID,
			Name:     "ag-2",
			Platform: capability.PlatformAntigravity,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"access_token":              "tok_xxx",
				"intercept_warmup_requests": true,
			},
			Concurrency:    1,
			Priority:       1,
			Status:         billing.StatusActive,
			Schedulable:    true,
			ProviderGroups: []providercore.GroupMembership{{ProviderID: providerID, GroupID: groupID}},
		},
	}

	h, cleanup := newTestGatewayHandler(t, group, []*gatewayprovider.ExecutionProvider{provider})
	defer cleanup()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body := []byte(`{
		"model": "claude-sonnet-4-5",
		"max_tokens": 256,
		"messages": [{"role":"user","content":[{"type":"text","text":"Warmup"}]}]
	}`)
	req := httptest.NewRequest("POST", "/antigravity/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// 模拟 routes/gateway.go 里的 ForcePlatform 中间件效果：
	// - 写入 request.Context（Service读取）
	// - 写入 gin.Context（Handler快速读取）
	ctx := requeststate.WithGroup(req.Context(), group)
	ctx = apikey.WithForcePlatform(ctx, capability.PlatformAntigravity)
	req = req.WithContext(ctx)
	c.Request = req
	c.Set(string(keyhttp.ContextKeyForcePlatform), capability.PlatformAntigravity)

	apiKey := &apikey.APIKey{
		ID:      3002,
		UserID:  4002,
		GroupID: &groupID,
		Status:  billing.StatusActive,
		User: &identity.User{
			ID:          4002,
			Concurrency: 10,
			Balance:     100,
		},
		Group: group,
	}

	c.Set(string(keyhttp.ContextKeyAPIKey), apiKey)
	c.Set(string(authctx.ContextKeyUser), authctx.AuthSubject{UserID: apiKey.UserID, Concurrency: 10})

	h.Messages(c)

	require.Equal(t, 200, rec.Code)

	selected, ok := c.Get(gatewayhttp.OpsProviderIDKey)
	require.True(t, ok)
	require.Equal(t, providerID, selected)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Regexp(t, `^msg_01[0-9A-Za-z]{22}$`, resp["id"])
	require.Equal(t, "claude-sonnet-4-5", resp["model"])
}

// AcquireBucketLease 夹具提供锁持有者句柄，并控制获取失败与等待结果。
func (f *fakeSchedulerCache) AcquireBucketLease(ctx context.Context, bucket scheduler.SchedulerBucket, ttl time.Duration) (*scheduler.BucketLease, bool, error) {
	ok, err := f.TryLockBucket(ctx, bucket, ttl)
	if err != nil || !ok {
		return nil, ok, err
	}
	return scheduler.NewBucketLease(func(cleanup context.Context) error { return f.UnlockBucket(cleanup, bucket) }), true, nil
}
