package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	media "github.com/TokenFlux/TokenRouter/internal/gateway/media"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	settingscore "github.com/TokenFlux/TokenRouter/internal/settings"
	settingstestkit "github.com/TokenFlux/TokenRouter/internal/settings/testkit"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayService_HandleOpenAIProviderUpstreamError_ImageRateLimitDoesNotBlockWholeProvider(t *testing.T) {
	repo := &gatewaytestkit.ModelHealthStore{}
	svc := newImagesFixture(imagesFixtureInputs{observer: gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{Store: repo, Options: providercore.HealthOptions{}})})
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 203, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}}
	body := []byte(`{"error":{"type":"rate_limit_exceeded","message":"Rate limit reached for gpt-image-2-codex (for limit gpt-image) on input-images per min. Please try again in 1s."}}`)

	disabled := gatewayprovider.ApplyOpenAIResponseHealth(context.Background(), svc.Output.Health, provider, http.StatusTooManyRequests, http.Header{}, body, false, "gpt-image-2").StopScheduling

	require.False(t, disabled)
	require.Len(t, repo.ModelRateLimitCalls, 1)
	require.Equal(t, providercore.OpenAIImageGenerationRateLimitKey, repo.ModelRateLimitCalls[0].Scope)
	require.False(t, svc.Output.Health.Runtime.Blocked(provider.Record.ID, func() string {
		return providercore.RefreshCredentialIdentity(gatewayprovider.ExecutionRecord(provider))
	}))
}

func TestOpenAIGatewayServiceForwardImages_ImageRateLimitReturnsFailoverAndCoolsCapability(t *testing.T) {
	repo := &gatewaytestkit.ModelHealthStore{}
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat"}`)
	errorBody := `{"error":{"type":"rate_limit_exceeded","message":"Rate limit reached for gpt-image-2-codex (for limit gpt-image) in organization org on input-images per min: Limit 4000, Used 4000. Please try again in 1s."}}`

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := newImagesFixture(imagesFixtureInputs{observer: gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{Store: repo, Options: providercore.HealthOptions{}}), transport: &auxiliaryHTTPRecorder{
		resp: &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"X-Request-Id": []string{"req_img_rate_limited"}},
			Body:       io.NopCloser(strings.NewReader(errorBody)),
		},
	}})
	parsed, err := media.ParseImageRequest(c.Request.URL.Path, c.GetHeader("Content-Type"), body, true)
	require.NoError(t, err)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 204,
			Name:     "openai-oauth",
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"access_token": "token-123",
			},
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, provider, body, parsed, "")

	require.Nil(t, result)
	var failoverErr *forwardcore.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "input-images per min")
	require.Len(t, repo.ModelRateLimitCalls, 1)
	require.Equal(t, providercore.OpenAIImageGenerationRateLimitKey, repo.ModelRateLimitCalls[0].Scope)
}

// TestOpenAIGatewayServiceForwardImages_TextFallbackDoesNotCoolImageCapability 验证上游仅返回文字时，提供商图片能力状态保持原样（#6171）。
// 文字结果按可重试的 502 换号，冷却依据是结构化上游错误。如果为每次文字结果设置 30 分钟冷却，换号会依次冷却整个池。
func TestOpenAIGatewayServiceForwardImages_TextFallbackDoesNotCoolImageCapability(t *testing.T) {
	repo := &gatewaytestkit.ModelHealthStore{}
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat"}`)
	upstreamSSE := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"status\":\"completed\",\"model\":\"gpt-5.4-mini\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"Here's a polished image prompt for your request.\"}]}]}}\n\n"

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := newImagesFixture(imagesFixtureInputs{store: repo, transport: &auxiliaryHTTPRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
		},
	}})
	parsed, err := media.ParseImageRequest(c.Request.URL.Path, c.GetHeader("Content-Type"), body, true)
	require.NoError(t, err)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 205,
			Name:     "openai-oauth",
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"access_token": "token-123",
			},
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, provider, body, parsed, "")

	require.Nil(t, result)
	require.Error(t, err)
	var failoverErr *forwardcore.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.False(t, failoverErr.RetryableOnSameProvider)
	// 换号行为不变：该判据仍足以放弃本提供商重试这一次请求……
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	// 提供商状态保持原样，重试过程中各提供商的冷却时间也保持原样。
	require.Empty(t, repo.ModelRateLimitCalls,
		"模型回文字只说明这一轮没出图，不构成提供商 30 分钟不可用的证据")
}

// TestOpenAIGatewayServiceForwardImages_StructuredUnavailableCoolsImageCapability 验证对照不变式：上游 error 帧点名 image_generation_unavailable 时仍写冷却，
// 保证 #6171 的修复没有把这项能力保护整个废掉。
func TestOpenAIGatewayServiceForwardImages_StructuredUnavailableCoolsImageCapability(t *testing.T) {
	repo := &gatewaytestkit.ModelHealthStore{}
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat"}`)
	upstreamSSE := "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"r\",\"error\":" +
		"{\"type\":\"upstream_error\",\"code\":\"image_generation_unavailable\"," +
		"\"message\":\"image generation tool is not available for this provider\"}}}\n\n"

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := newImagesFixture(imagesFixtureInputs{store: repo, transport: &auxiliaryHTTPRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
		},
	}})
	parsed, err := media.ParseImageRequest(c.Request.URL.Path, c.GetHeader("Content-Type"), body, true)
	require.NoError(t, err)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 206,
			Name:     "openai-oauth",
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"access_token": "token-123",
			},
		},
	}

	before := time.Now()
	result, err := svc.ForwardImages(context.Background(), c, provider, body, parsed, "")

	require.Nil(t, result)
	require.Error(t, err)
	require.Len(t, repo.ModelRateLimitCalls, 1)
	call := repo.ModelRateLimitCalls[0]
	require.Equal(t, provider.Record.ID, call.ProviderID)
	require.Equal(t, providercore.OpenAIImageGenerationRateLimitKey, call.Scope)
	require.Equal(t, openai.OpenAIImagesOAuthUnavailableReason, call.Reason)
	require.WithinDuration(t, before.Add(openai.OpenAIImagesOAuthUnavailableDefaultCooldown), call.ResetAt, time.Second)
}

func TestOpenAIGatewayService_CoolOpenAIImagesOAuthToolUsesConfiguredCooldown(t *testing.T) {
	providerRepo := &gatewaytestkit.ModelHealthStore{}
	settingRepo := settingstestkit.NewMemory()
	settingRepo.Data[providercore.SettingKeyOpenAIImagesOAuthUnavailableCooldownSettings] = `{"cooldown_minutes":7}`
	svc := newImagesFixture(imagesFixtureInputs{store: providerRepo})
	svc.Cooldown.Settings = providercore.NewRuntimeSettings(settingRepo, settingscore.ErrSettingNotFound).GetOpenAIImagesOAuthUnavailableCooldownSettings

	before := time.Now()
	svc.Cooldown.Apply(context.Background(), &providercore.Record{LoadLocation: time.LoadLocation, ID: 206, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth})

	require.Len(t, providerRepo.ModelRateLimitCalls, 1)
	require.WithinDuration(t, before.Add(7*time.Minute), providerRepo.ModelRateLimitCalls[0].ResetAt, time.Second)
}

func TestOpenAIGatewayServiceForwardImages_CapabilityLossCoolsImageScope(t *testing.T) {
	repo := &gatewaytestkit.ModelHealthStore{}
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat"}`)
	errorBody := `{"error":{"message":"Tool choice 'image_generation' not found in 'tools' parameter.","param":"tool_choice","type":"invalid_request_error"}}`

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := newImagesFixture(imagesFixtureInputs{observer: gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{Store: repo, Options: providercore.HealthOptions{}}), transport: &auxiliaryHTTPRecorder{
		resp: &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"X-Request-Id": []string{"req_img_capability_lost"}},
			Body:       io.NopCloser(strings.NewReader(errorBody)),
		},
	}})
	parsed, err := media.ParseImageRequest(c.Request.URL.Path, c.GetHeader("Content-Type"), body, true)
	require.NoError(t, err)
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 205,
			Name:     "openai-oauth",
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"access_token": "token-123",
			},
		},
	}

	before := time.Now()
	result, err := svc.ForwardImages(context.Background(), c, provider, body, parsed, "")

	require.Nil(t, result)
	require.Error(t, err)
	require.Len(t, repo.ModelRateLimitCalls, 1)
	call := repo.ModelRateLimitCalls[0]
	require.Equal(t, provider.Record.ID, call.ProviderID)
	require.Equal(t, providercore.OpenAIImageGenerationRateLimitKey, call.Scope)
	require.Equal(t, providercore.OpenAIImageCapabilityLossReason, call.Reason)
	require.WithinDuration(t, before.Add(providercore.OpenAIImageCapabilityLossCooldown), call.ResetAt, time.Second)
}

func TestOpenAIGatewayServiceHandleUpstreamError_PassthroughCapabilityLossDoesNotCool(t *testing.T) {
	repo := &gatewaytestkit.ModelHealthStore{}
	svc := newImagesFixture(imagesFixtureInputs{observer: gatewaytestkit.NewHealthObserver(gatewaytestkit.HealthInput{Store: repo, Options: providercore.HealthOptions{}})})
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 206, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}}
	body := []byte(`{"error":{"message":"Tool choice 'image_generation' not found in 'tools' parameter.","param":"tool_choice","type":"invalid_request_error"}}`)

	disabled := gatewayprovider.ApplyOpenAIResponseHealth(context.Background(), svc.Output.Health, provider, http.StatusBadRequest, http.Header{}, body, false, "gpt-5.5").StopScheduling

	require.False(t, disabled)
	require.Empty(t, repo.ModelRateLimitCalls)
	require.False(t, svc.Output.Health.Runtime.Blocked(provider.Record.ID, func() string {
		return providercore.RefreshCredentialIdentity(gatewayprovider.ExecutionRecord(provider))
	}))
}
