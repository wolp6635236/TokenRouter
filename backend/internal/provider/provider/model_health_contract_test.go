package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/gateway/media"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

type modelNotFoundRateLimitCall struct {
	providerID int64
	scope      string
	resetAt    time.Time
	reason     string
}

type modelNotFoundProviderRepoStub struct {
	providercore.HealthStore
	tempCalls           int
	modelRateLimitCalls []modelNotFoundRateLimitCall
	modelRateLimitErr   error
}

func (r *modelNotFoundProviderRepoStub) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.tempCalls++
	return nil
}

func (r *modelNotFoundProviderRepoStub) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	call := modelNotFoundRateLimitCall{
		providerID: id,
		scope:      scope,
		resetAt:    resetAt,
	}
	if len(reason) > 0 {
		call.reason = reason[0]
	}
	r.modelRateLimitCalls = append(r.modelRateLimitCalls, call)
	return r.modelRateLimitErr
}

func TestRateLimitService_HandleUpstreamError_ModelNotFoundUsesModelRateLimit(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAIModelNotFoundTempProvider()

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusNotFound, http.Header{}, []byte(`{"error":{"code":"model_not_found","message":"model not found"}}`), "gpt-5.4")).StopScheduling

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	call := repo.modelRateLimitCalls[0]
	require.Equal(t, provider.ID, call.providerID)
	require.Equal(t, "gpt-5.4", call.scope)
	require.Equal(t, providercore.ModelNotFoundReason, call.reason)
	require.WithinDuration(t, time.Now().Add(providercore.ModelNotFoundCooldown), call.resetAt, 5*time.Second)
}

// TestRateLimitService_PoolModeModelNotFoundSkipsDefaultModelPause 验证池模式不会根据
// 聚合上游的一次 404 暂停本地提供商与模型组合。
func TestRateLimitService_PoolModeModelNotFoundSkipsDefaultModelPause(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := &providercore.Record{
		ID:          102,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeAPIKey,
		Status:      billing.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"pool_mode": true},
	}

	decision := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusNotFound, http.Header{}, []byte(`{"error":{"code":"model_not_found","message":"model not found"}}`), "gpt-5.4"))

	require.Equal(t, providercore.ErrorPolicyPoolBypassed, decision.Policy)
	require.False(t, decision.StopScheduling)
	require.Zero(t, repo.tempCalls)
	require.Empty(t, repo.modelRateLimitCalls)
}

func TestRateLimitService_HandleUpstreamError_ModelNotFoundWriteFailureDoesNotTempUnschedule(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{modelRateLimitErr: errors.New("write failed")}
	svc := newModelHealthObserver(repo)
	provider := openAIModelNotFoundTempProvider()

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusNotFound, http.Header{}, []byte(`{"error":{"code":"model_not_found","message":"model not found"}}`), "gpt-5.4")).StopScheduling

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
}

func TestRateLimitService_HandleUpstreamError_Bare404UsesModelScopedTempUnschedulableWhenModelKnown(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAIModelNotFoundTempProvider()

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusNotFound, http.Header{}, []byte(`{"error":{"message":"endpoint not found"}}`), "gpt-5.4")).StopScheduling

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	call := repo.modelRateLimitCalls[0]
	require.Equal(t, provider.ID, call.providerID)
	require.Equal(t, "gpt-5.4", call.scope)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), call.resetAt, 5*time.Second)

	var state providercore.TempUnschedState
	require.NoError(t, json.Unmarshal([]byte(call.reason), &state))
	require.Equal(t, http.StatusNotFound, state.StatusCode)
	require.Equal(t, "not found", state.MatchedKeyword)
}

func TestRateLimitService_HandleUpstreamError_Bare404WithoutModelKeepsProviderTempUnschedulable(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAIModelNotFoundTempProvider()

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusNotFound, http.Header{}, []byte(`{"error":{"message":"endpoint not found"}}`))).StopScheduling

	require.True(t, handled)
	require.Equal(t, 1, repo.tempCalls)
	require.Empty(t, repo.modelRateLimitCalls)
}

func TestRateLimitService_HandleUpstreamError_ModelTempWriteFailureNeverWidensToProvider(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{modelRateLimitErr: errors.New("write failed")}
	svc := newModelHealthObserver(repo)
	provider := openAIModelNotFoundTempProvider()

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusNotFound, http.Header{}, []byte(`{"error":{"message":"endpoint not found"}}`), "gpt-5.4")).StopScheduling

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
}

func TestRateLimitService_HandleTempUnschedulable_PoolModeAppliesExplicitModelRule(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAIModelNotFoundTempProvider()
	provider.Credentials["pool_mode"] = true

	handled := svc.Core.HandleTempUnschedulable(context.Background(), provider, http.StatusNotFound, []byte(`{"error":{"message":"endpoint not found"}}`), "gpt-5.4")

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "gpt-5.4", repo.modelRateLimitCalls[0].scope)
}

func TestRateLimitService_HandleTempUnschedulable_PoolModeCustomPolicyUsesModelScope(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAIModelNotFoundTempProvider()
	provider.Credentials["pool_mode"] = true
	provider.Credentials["custom_error_codes_enabled"] = true
	provider.Credentials["custom_error_codes"] = []any{float64(http.StatusNotFound)}

	handled := svc.Core.HandleTempUnschedulable(context.Background(), provider, http.StatusNotFound, []byte(`{"error":{"message":"endpoint not found"}}`), "gpt-5.4")

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "gpt-5.4", repo.modelRateLimitCalls[0].scope)
}

func TestRateLimitService_HandleUpstreamError_CustomPolicyExclusionSkipsAllState(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAIModelNotFoundTempProvider()
	provider.Credentials["custom_error_codes_enabled"] = true
	provider.Credentials["custom_error_codes"] = []any{float64(http.StatusServiceUnavailable)}

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusNotFound, http.Header{}, []byte(`{"error":{"message":"endpoint not found"}}`), "gpt-5.4")).StopScheduling

	require.False(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Empty(t, repo.modelRateLimitCalls)
}

func TestRateLimitService_HandleTempUnschedulable_AuthenticationFailureStaysProviderScoped(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAIModelNotFoundTempProvider()
	provider.TempUnschedulableReason = "legacy non-JSON reason"
	provider.Credentials["temp_unschedulable_rules"] = []any{
		map[string]any{
			"error_code":       float64(http.StatusUnauthorized),
			"keywords":         []any{"unauthorized"},
			"duration_minutes": float64(10),
		},
	}

	handled := svc.Core.HandleTempUnschedulable(context.Background(), provider, http.StatusUnauthorized, []byte(`{"error":{"message":"unauthorized"}}`), "gpt-5.4")

	require.True(t, handled)
	require.Equal(t, 1, repo.tempCalls)
	require.Empty(t, repo.modelRateLimitCalls)
}

func openAIModelNotFoundTempProvider() *providercore.Record {
	return &providercore.Record{
		ID:          101,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeAPIKey,
		Status:      billing.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       float64(http.StatusNotFound),
					"keywords":         []any{"not found"},
					"duration_minutes": float64(10),
				},
			},
		},
	}
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedModelUsesModelRateLimit(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAICodexPlanGatedOAuthProvider()

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusBadRequest, http.Header{}, []byte(`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT provider."}`), "gpt-5.6-sol")).StopScheduling

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	call := repo.modelRateLimitCalls[0]
	require.Equal(t, provider.ID, call.providerID)
	require.Equal(t, "gpt-5.6-sol", call.scope)
	require.Equal(t, providercore.CodexPlanGatedModelReason, call.reason)
	require.WithinDuration(t, time.Now().Add(providercore.CodexPlanGatedModelCooldown), call.resetAt, 5*time.Second)
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedModelUsesFinalUpstreamModel(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAICodexPlanGatedOAuthProvider()
	// 状态入口接收实际发送的最终上游 ID，即使该 ID 也是映射键也不得再次映射。
	provider.Credentials["model_mapping"] = map[string]any{
		"gpt-5.6-sol":       "external-model-v1",
		"external-model-v1": "unexpected-remap",
	}

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusBadRequest, http.Header{}, []byte(`{"detail":"The 'external-model-v1' model is not supported when using Codex with a ChatGPT provider."}`), "external-model-v1")).StopScheduling

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "external-model-v1", repo.modelRateLimitCalls[0].scope)
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedModelIgnoresAPIKeyProvider(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAICodexPlanGatedOAuthProvider()
	provider.Type = capability.ProviderTypeAPIKey

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusBadRequest, http.Header{}, []byte(`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT provider."}`), "gpt-5.6-sol")).StopScheduling

	require.False(t, handled)
	require.Empty(t, repo.modelRateLimitCalls)
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedImageModelSkipsCooldown(t *testing.T) {
	for _, model := range []string{"gpt-image-1", "gpt-image-1.5", "gpt-image-2"} {
		t.Run(model, func(t *testing.T) {
			repo := &modelNotFoundProviderRepoStub{}
			svc := newModelHealthObserver(repo)
			provider := openAICodexPlanGatedOAuthProvider()

			handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusBadRequest, http.Header{}, []byte(`{"detail":"The '`+model+`' model is not supported when using Codex with a ChatGPT provider."}`), model)).StopScheduling

			require.True(t, handled, "当前文本端点尝试仍应切换提供商")
			require.Empty(t, repo.modelRateLimitCalls, "文本端点错配不应冷却提供商的图片模型")
			require.Zero(t, repo.tempCalls)
		})
	}
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedTextModelStillCoolsDown(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAICodexPlanGatedOAuthProvider()

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusBadRequest, http.Header{}, []byte(`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT provider."}`), "gpt-5.6-sol")).StopScheduling

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1, "非图片套餐门控模型应保留原有冷却")
	require.Equal(t, providercore.CodexPlanGatedModelReason, repo.modelRateLimitCalls[0].reason)
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedImageModelKeepsCooldownOnImagesEndpoint(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAICodexPlanGatedOAuthProvider()

	handled := svc.ApplyUpstreamError(requeststate.WithOpenAIImagesEndpoint(context.Background()), provider, healthTestObservation(requeststate.WithOpenAIImagesEndpoint(context.Background()), http.StatusBadRequest, http.Header{}, []byte(`{"detail":"The 'gpt-image-2' model is not supported when using Codex with a ChatGPT provider."}`), "gpt-image-2")).StopScheduling

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1, "专用 Images 端点上的能力拒绝必须保留冷却")
	require.Equal(t, "gpt-image-2", repo.modelRateLimitCalls[0].scope)
	require.Equal(t, providercore.CodexPlanGatedModelReason, repo.modelRateLimitCalls[0].reason)
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedImageModelSkipsCooldownOnIntentOnly(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAICodexPlanGatedOAuthProvider()

	handled := svc.ApplyUpstreamError(requeststate.WithOpenAIImageGenerationIntent(context.Background()), provider, healthTestObservation(requeststate.WithOpenAIImageGenerationIntent(context.Background()), http.StatusBadRequest, http.Header{}, []byte(`{"detail":"The 'gpt-image-2' model is not supported when using Codex with a ChatGPT provider."}`), "gpt-image-2")).StopScheduling

	require.True(t, handled)
	require.Empty(t, repo.modelRateLimitCalls, "生图意图不等于专用 Images 入口")
}

func TestRateLimitService_HandleUpstreamError_ModelNotFoundImageModelStillCoolsDown(t *testing.T) {
	repo := &modelNotFoundProviderRepoStub{}
	svc := newModelHealthObserver(repo)
	provider := openAICodexPlanGatedOAuthProvider()

	handled := svc.ApplyUpstreamError(context.Background(), provider, healthTestObservation(context.Background(), http.StatusNotFound, http.Header{}, []byte(`{"error":{"message":"The model 'gpt-image-2' does not exist","code":"model_not_found"}}`), "gpt-image-2")).StopScheduling

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1, "图片模型的 404 model_not_found 仍应冷却")
	require.Equal(t, providercore.ModelNotFoundReason, repo.modelRateLimitCalls[0].reason)
}

func openAICodexPlanGatedOAuthProvider() *providercore.Record {
	return &providercore.Record{
		ID:          202,
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Schedulable: true,
		Credentials: map[string]any{},
	}
}

// newModelHealthObserver 为生产观测入口绑定测试依赖和请求输入。
func newModelHealthObserver(repo *modelNotFoundProviderRepoStub) *UpstreamHealth {
	core := providercore.NewHealthService(repo, nil, providercore.HealthOptions{})
	models := &ModelHealth{Health: core, IsImageModel: media.IsGPTImageGenerationModel}
	return &UpstreamHealth{Core: core, Models: models, Limits: &RateLimitObserver{Health: core}}
}

// healthTestObservation 将测试请求的模型意图转换为观测字段。
func healthTestObservation(ctx context.Context, status int, headers http.Header, body []byte, models ...string) HealthObservation {
	input := HealthObservation{Status: status, Headers: headers, Body: body, ModelProvided: len(models) > 0, ImagesEndpoint: requeststate.OpenAIImagesEndpointFromContext(ctx)}
	if len(models) > 0 {
		input.Model = models[0]
		input.EffectiveModel = strings.TrimSpace(models[0])
	}
	if thinking, ok := requeststate.ThinkingEnabledFromContext(ctx); ok {
		input.Thinking = &thinking
	}
	return input
}
