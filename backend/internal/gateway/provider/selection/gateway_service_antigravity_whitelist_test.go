package selection

import (
	"context"
	"strings"
	"testing"
	time "time"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func TestGatewayService_isModelSupportedByProvider_AntigravityModelMapping(t *testing.T) {
	// 使用 model_mapping 作为白名单（通配符匹配）
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity,
			Credentials: map[string]any{
				"model_whitelist": []string{"claude-sonnet-4-5", "gemini-3-flash"},
				"model_mapping": map[string]any{
					"claude-*":   "claude-sonnet-4-5",
					"gemini-3-*": "gemini-3-flash",
				},
			},
		},
	}

	// claude-* 通配符匹配
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "claude-sonnet-4-5"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "claude-haiku-4-5"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "claude-opus-4-6"))

	// gemini-3-* 通配符匹配
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "gemini-3-flash"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "gemini-3-pro-high"))

	// gemini-2.5-* 不匹配（不在 model_mapping 中）
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "gemini-2.5-flash"))
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "gemini-2.5-pro"))

	// 其他平台模型不支持
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "gpt-4"))

	// 空模型允许
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), ""))
}

func TestGatewayService_isModelSupportedByProvider_AntigravityNoMapping(t *testing.T) {
	// 未配置 model_mapping 时，使用默认映射（domain.DefaultAntigravityModelMapping）
	// 只有默认映射中的模型才被支持
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity,
			Credentials: map[string]any{},
		},
	}

	// 默认映射中的模型应该被支持
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "claude-sonnet-4-5"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "gemini-3-flash"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "gemini-2.5-pro"))
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "claude-haiku-4-5"))

	// 不在默认映射中的模型不被支持
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "claude-3-5-sonnet-20241022"))
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "claude-unknown-model"))

	// 非 claude-/gemini- 前缀仍然不支持
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "gpt-4"))
}

// TestGatewayService_isModelSupportedByProviderWithContext_ThinkingMode 测试 thinking 模式下的模型支持检查
// 验证调度时使用映射后的最终模型名（包括 thinking 后缀）来检查 model_mapping 支持
func TestGatewayService_isModelSupportedByProviderWithContext_ThinkingMode(t *testing.T) {
	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{}, Shared: Shared{}},

		nil)

	tests := []struct {
		name            string
		modelMapping    map[string]any
		requestedModel  string
		thinkingEnabled bool
		expected        bool
	}{
		// 场景 1: 只配置 claude-sonnet-4-5-thinking，请求 claude-sonnet-4-5 + thinking=true
		// 白名单按规范化后的最终模型判断。
		{
			name: "thinking_enabled_matches_final_whitelist",
			modelMapping: map[string]any{
				"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: true,
			expected:        true,
		},
		// 场景 2: 只配置 claude-sonnet-4-5-thinking，请求 claude-sonnet-4-5 + thinking=false
		// 白名单按规范化后的最终模型判断。
		{
			name: "thinking_disabled_no_base_mapping_returns_false",
			modelMapping: map[string]any{
				"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: false,
			expected:        false,
		},
		// 场景 3: 配置 claude-sonnet-4-5（非 thinking），请求 claude-sonnet-4-5 + thinking=true
		// 最终模型名 = claude-sonnet-4-5-thinking，不在 mapping 中，应该不匹配
		{
			name: "thinking_enabled_no_match_non_thinking_mapping",
			modelMapping: map[string]any{
				"claude-sonnet-4-5": "claude-sonnet-4-5",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: true,
			expected:        false,
		},
		// 场景 4: 配置两种模型，请求 claude-sonnet-4-5 + thinking=true，应该匹配 thinking 版本
		{
			name: "both_models_thinking_enabled_matches_thinking",
			modelMapping: map[string]any{
				"claude-sonnet-4-5":          "claude-sonnet-4-5",
				"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: true,
			expected:        true,
		},
		// 场景 5: 配置两种模型，请求 claude-sonnet-4-5 + thinking=false，应该匹配非 thinking 版本
		{
			name: "both_models_thinking_disabled_matches_non_thinking",
			modelMapping: map[string]any{
				"claude-sonnet-4-5":          "claude-sonnet-4-5",
				"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: false,
			expected:        true,
		},
		// 场景 6: 通配符 claude-* 应该同时匹配 thinking 和非 thinking
		{
			name: "wildcard_matches_thinking",
			modelMapping: map[string]any{
				"claude-*": "claude-sonnet-4-5",
			},
			requestedModel:  "claude-sonnet-4-5",
			thinkingEnabled: true,
			expected:        true, // claude-sonnet-4-5-thinking 匹配 claude-*
		},
		// 场景 7: 只配置 thinking 变体但没有基础模型映射 → 返回 false
		// Opus 4.6 不自动追加 thinking 后缀，基础名称仍须在白名单内。
		{
			name: "opus_without_suffix_rewrite_does_not_match_thinking_only_scope",
			modelMapping: map[string]any{
				"claude-opus-4-6-thinking": "claude-opus-4-6-thinking",
			},
			requestedModel:  "claude-opus-4-6",
			thinkingEnabled: true,
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &gatewayprovider.ExecutionProvider{
				Record: providercore.Record{
					LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity,
					Credentials: map[string]any{
						"model_whitelist": func() []string {
							var out []string
							for source, value := range tt.modelMapping {
								if strings.Contains(source, "*") {
									out = append(out, source)
									continue
								}
								if model, ok := value.(string); ok {
									out = append(out, model)
								}
							}
							return out
						}(),
						"model_mapping": tt.modelMapping,
					},
				},
			}

			ctx := requeststate.WithThinkingEnabled(context.Background(), tt.thinkingEnabled)
			result := svc.isModelSupportedByProviderWithContext(ctx, provider, tt.requestedModel)

			require.Equal(t, tt.expected, result,
				"isModelSupportedByProviderWithContext(ctx[thinking=%v], provider, %q) = %v, want %v",
				tt.thinkingEnabled, tt.requestedModel, result, tt.expected)
		})
	}
}

// TestGatewayService_isModelSupportedByProvider_CustomMappingNotInDefault 测试自定义模型映射中
// 不在 DefaultAntigravityModelMapping 中的模型能通过调度
func TestGatewayService_isModelSupportedByProvider_CustomMappingNotInDefault(t *testing.T) {
	// 自定义映射中包含不在默认映射中的模型
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"my-custom-model":   "actual-upstream-model",
					"gpt-4o":            "some-upstream-model",
					"llama-3-70b":       "llama-3-70b-upstream",
					"claude-sonnet-4-5": "claude-sonnet-4-5",
				},
			},
		},
	}

	// 自定义模型应该通过（不在 DefaultAntigravityModelMapping 中也可以）
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "my-custom-model"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "gpt-4o"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "llama-3-70b"))
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "claude-sonnet-4-5"))

	// 不在自定义映射中的模型不通过
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "gpt-3.5-turbo"))
	require.False(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), "unknown-model"))

	// 空模型允许
	require.True(t, gatewayprovider.ExecutionModelPolicy(provider).Supports(context.Background(), ""))
}

// TestGatewayService_isModelSupportedByProviderWithContext_CustomMappingThinking
// 测试自定义映射 + thinking 模式的交互
func TestGatewayService_isModelSupportedByProviderWithContext_CustomMappingThinking(t *testing.T) {
	svc := newGenericSelectionForTest(GenericDependencies{Reads: Reads{}, Shared: Shared{}},

		nil)

	// 自定义映射同时配置基础模型和 thinking 变体
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformAntigravity,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-sonnet-4-5":          "claude-sonnet-4-5",
					"claude-sonnet-4-5-thinking": "claude-sonnet-4-5-thinking",
					"my-custom-model":            "upstream-model",
				},
			},
		},
	}

	// thinking=true: claude-sonnet-4-5 → mapped=claude-sonnet-4-5 → +thinking → check IsModelSupported(claude-sonnet-4-5-thinking)=true
	ctx := requeststate.WithThinkingEnabled(context.Background(), true)
	require.True(t, svc.isModelSupportedByProviderWithContext(ctx, provider, "claude-sonnet-4-5"))

	// thinking=false: claude-sonnet-4-5 → mapped=claude-sonnet-4-5 → check IsModelSupported(claude-sonnet-4-5)=true
	ctx = requeststate.WithThinkingEnabled(context.Background(), false)
	require.True(t, svc.isModelSupportedByProviderWithContext(ctx, provider, "claude-sonnet-4-5"))

	// 自定义模型（非 claude）不受 thinking 后缀影响，mapped 成功即通过
	ctx = requeststate.WithThinkingEnabled(context.Background(), true)
	require.True(t, svc.isModelSupportedByProviderWithContext(ctx, provider, "my-custom-model"))
}
