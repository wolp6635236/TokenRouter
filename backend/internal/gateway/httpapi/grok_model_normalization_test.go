package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	xai "github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/stretchr/testify/require"
)

// grokModelStateProviderRepo 在测试中记录 Grok 模型级状态。
type grokModelStateProviderRepo struct {
	gatewayprovider.ExecutionProviderStore

	modelRateLimitCalls []grokModelRateLimitCall
}

// grokModelRateLimitCall 保存一次模型限流写入的关键字段。
type grokModelRateLimitCall struct {
	providerID int64
	scope      string
	resetAt    time.Time
	reason     string
}

// SetModelRateLimit 记录 Grok 模型级状态写入，供规范模型键回归测试断言。
func (r *grokModelStateProviderRepo) SetModelRateLimit(_ context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	call := grokModelRateLimitCall{providerID: id, scope: scope, resetAt: resetAt}
	if len(reason) > 0 {
		call.reason = reason[0]
	}
	r.modelRateLimitCalls = append(r.modelRateLimitCalls, call)
	return nil
}

func TestGrokFinalUpstreamModelNormalization(t *testing.T) {
	tests := []struct {
		name     string
		provider *gatewayprovider.ExecutionProvider
		model    string
		want     string
	}{
		{
			name:     "oauth normalizes builtin alias",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}},
			model:    "grok",
			want:     "grok",
		},
		{
			name:     "api key normalizes builtin alias",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok, Type: capability.ProviderTypeAPIKey}},
			model:    " grok-latest ",
			want:     "grok-latest",
		},
		{
			name:     "grok oauth does not use codex normalization",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}},
			model:    "gpt-5.6",
			want:     "gpt-5.6",
		},
		{
			name:     "unknown model passes through",
			provider: &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok, Type: capability.ProviderTypeAPIKey}},
			model:    "custom-grok-model",
			want:     "custom-grok-model",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, gatewayprovider.ExecutionModelPolicy(test.provider).NormalizeOpenAI(test.model))
		})
	}
}

// TestGrokExplicitMappingPrecedesBuiltinNormalization 验证提供商映射目标随后才执行平台别名解析。
func TestGrokExplicitMappingPrecedesBuiltinNormalization(t *testing.T) {
	direct := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
			Type: capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"grok": "grok-4.3"},
			},
		},
	}
	require.Equal(t, "grok-4.3", gatewayprovider.ExecutionModelPolicy(direct).OpenAIUpstream("grok", false))

	aliasTarget := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
			Type: capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"client-alias": "grok-latest"},
			},
		},
	}
	require.Equal(t, "grok-latest", gatewayprovider.ExecutionModelPolicy(aliasTarget).OpenAIUpstream("client-alias", false))
	require.Equal(t, "grok-latest", gatewayprovider.ExecutionModelPolicy(aliasTarget).UpstreamModel(context.Background(), "client-alias"))
}

// TestGrokRuntimeModelKeysUseFinalUpstreamID 验证封禁与限流状态不会按别名重复建键。
func TestGrokRuntimeModelKeysUseFinalUpstreamID(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
			Type: capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"client-alias": "grok-latest",
					"grok-4.5":     "grok-4.3",
				},
			},
		},
	}

	require.Equal(t, "grok", gatewayprovider.ExecutionModelPolicy(provider).CanonicalSchedulingModel("grok"))
	require.Equal(t, "grok-latest", gatewayprovider.ExecutionModelPolicy(provider).CanonicalSchedulingModel("client-alias"))
	require.Equal(t, []string{"grok"}, gatewayprovider.ExecutionModelPolicy(provider).LimitKeys(context.Background(), "grok"))
	require.Equal(t, "grok", (&provideradapter.ModelHealth{}).LimitKey(gatewayprovider.ExecutionRecord(provider), "grok", nil))
	// 状态处理接收最终上游模型后不得再次命中 grok-4.5 -> grok-4.3。
	require.Equal(t, xai.DefaultResponsesModel, (&provideradapter.ModelHealth{}).LimitKey(gatewayprovider.ExecutionRecord(provider), xai.DefaultResponsesModel, nil))
}

// TestGrokModelNotFoundWritesFinalUpstreamID 验证 Grok 默认错误处理写入最终上游模型键。
func TestGrokModelNotFoundWritesFinalUpstreamID(t *testing.T) {
	repo := &grokModelStateProviderRepo{}
	svc := newWSFixture(wsFixtureInputs{health: newUpstreamHealthForTest(repo, nil, nil, providercore.HealthOptions{}, nil)})
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 4511,
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"client-alias": "grok-latest",
					"grok-4.5":     "grok-4.3",
				},
			},
		},
	}
	providerMappedModel := gatewayprovider.ExecutionModelPolicy(provider).Mapped("client-alias")
	require.Equal(t, "grok-latest", providerMappedModel)

	decision := gatewayprovider.ApplyGrokExecutionHealth(context.Background(), svc.Output.GrokHealth, provider, http.StatusNotFound, nil, []byte(`{"error":{"code":"model_not_found","message":"model not found"}}`), "", providerMappedModel)

	require.True(t, decision.StopScheduling)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "grok-latest", repo.modelRateLimitCalls[0].scope)
	require.Equal(t, providercore.ModelNotFoundReason, repo.modelRateLimitCalls[0].reason)
	require.False(t, wsFixtureProviderBlocked(svc, provider))
}

// TestGrokTransientErrorBlocksOnlyFinalModel 验证 API Key 的连续瞬态错误只冷却最终模型。
func TestGrokTransientErrorBlocksOnlyFinalModel(t *testing.T) {
	repo := &grokModelStateProviderRepo{}
	svc := newWSFixture(wsFixtureInputs{health: newUpstreamHealthForTest(repo, nil, nil, providercore.HealthOptions{}, nil)})
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 4512,
			Platform: capability.PlatformGrok,
			Type:     capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"client-alias": "grok-latest",
					"grok-4.5":     "grok-4.3",
				},
			},
		},
	}
	canonicalModel := gatewayprovider.ExecutionModelPolicy(provider).NormalizeOpenAI(gatewayprovider.ExecutionModelPolicy(provider).Mapped("client-alias"))
	body := []byte(`{"error":{"message":"temporary upstream failure"}}`)

	first := gatewayprovider.ApplyGrokExecutionHealth(context.Background(), svc.Output.GrokHealth, provider, http.StatusBadGateway, nil, body, "", canonicalModel)
	second := gatewayprovider.ApplyGrokExecutionHealth(context.Background(), svc.Output.GrokHealth, provider, http.StatusBadGateway, nil, body, "", canonicalModel)

	require.False(t, first.StopScheduling)
	require.False(t, second.StopScheduling)
	require.False(t, wsFixtureProviderBlocked(svc, provider))
	require.True(t, wsFixtureModelBlocked(svc, provider, "client-alias"))
	require.False(t, wsFixtureModelBlocked(svc, provider, "grok-4.3"))
	require.Empty(t, repo.modelRateLimitCalls)
}

// TestGrokCountTokensUsesCanonicalModel 验证默认目录与内置别名表保持独立。

// TestGrokCountTokensUsesCanonicalModel 验证 count-tokens 转换记录映射模型并发送最终模型。
func TestGrokCountTokensUsesCanonicalModel(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
			Type: capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"claude-sonnet-4-5": "grok-latest"},
			},
		},
	}
	prepared, err := gatewayprovider.PrepareAnthropicInputTokens(
		[]byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`),
		provider,
		"",
	)
	require.NoError(t, err)
	require.Equal(t, "grok-latest", prepared.BillingModel)
	require.Equal(t, "grok-latest", prepared.UpstreamModel)
	require.Equal(t, "grok-latest", prepared.Request.Model)
}

// TestGrokWSModelUsesCanonicalID 验证 WebSocket HTTP bridge 使用相同的最终标准化入口。
func TestGrokWSModelUsesCanonicalID(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth, Credentials: map[string]any{}}}
	require.Equal(t, "grok", resolveGrokWSUpstreamModel(provider, []byte(`{"model":"grok"}`), "grok"))
	require.Equal(t, "grok", resolveGrokWSUpstreamModel(provider, []byte(`{"model":"grok"}`), ""))

	billingModel, upstreamModel := resolveGrokWSModels(provider, []byte(`{"model":"grok"}`), "")
	require.Equal(t, "grok", billingModel)
	require.Equal(t, "grok", upstreamModel)

	billingModel, upstreamModel = resolveGrokWSModels(provider, []byte(`{"input":"hello"}`), "")
	require.Empty(t, billingModel)
	require.Equal(t, xai.DefaultResponsesModel, upstreamModel)
}
