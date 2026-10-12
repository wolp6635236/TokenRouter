package app

// 本文件检查 gateway_messages_http.go 与 gateway_count_tokens.go 的预热请求拦截。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/config"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/identity/httpapi/authctx"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

func newTestGatewayHandler(t *testing.T, group *routing.Group, providers []*gatewayprovider.ExecutionProvider) (*messageEndpointsFixture, func()) {
	t.Helper()

	schedulerCache := &fakeSchedulerCache{providers: providers}
	schedulerSnapshot := scheduler.NewSnapshotService(schedulerCache, nil, nil, nil, nil, scheduler.SnapshotBindings{})

	gwSvc, gwSvcChoices, messages := newGenericExecutionAndSelectionFixture(
		nil, // providerRepo：调度快照命中，不读取仓库。
		&fakeGroupRepo{group: group},
		nil, // usageLogRepo
		nil, // cache：关闭粘性会话。
		nil, // cfg
		schedulerSnapshot,
		nil, // concurrencyService：关闭负载感知，获取并发槽位总是成功。
		nil, // healthObserver
		nil, // identityService
		nil, // httpUpstream
		nil, // deferredService
		nil, // messageCredentials
		nil, // sessionLimitCache
		nil, // windowCostCache
		nil, // rpmCache
		nil, // digestStore
		nil, // settingService
		nil, // tlsFPProfileService
		nil, // pricingConfigService
		nil, // resolver
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
		// /v1/messages（Claude 兼容）入口
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
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
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
	req := httptest.NewRequest(http.MethodPost, "/antigravity/v1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// 模拟 routes/gateway.go 里的 ForcePlatform 中间件效果：
	// - 写入 request.Context（Service 读取）
	// - 写入 gin.Context（Handler 快速读取）
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
