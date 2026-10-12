package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	"github.com/TokenFlux/TokenRouter/internal/gateway/media"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/ops"
	providerimages "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	upstreamcore "github.com/TokenFlux/TokenRouter/internal/upstream"
	upstreamopenai "github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

type openAIImagesReadErrorBody struct {
	err error
}

// countingModelRateLimitRepo 记录 SetModelRateLimit 调用，用于断言"没写提供商状态"。
type countingModelRateLimitRepo struct {
	gatewayprovider.ExecutionProviderStore
	calls  int
	scopes []string
}

func (b *openAIImagesReadErrorBody) Read([]byte) (int, error) { return 0, b.err }

func (b *openAIImagesReadErrorBody) Close() error { return nil }

func TestOpenAIImagesOAuthBodyReadTransportErrorFailover(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Request-Id": []string{"req_h2_read_failure"},
			"X-Upstream":   []string{"preserved"},
		},
		Body: &openAIImagesReadErrorBody{err: errors.New("stream error: stream ID 11; INTERNAL_ERROR; received from peer")},
	}
	provider := &gatewayprovider.ExecutionProvider{Record: providerimages.Record{LoadLocation: time.LoadLocation, ID: 5400, Name: "openai-oauth", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}}
	svc := newImagesFixture(imagesFixtureInputs{})

	_, _, _, readErr := upstreamopenai.ReadImagesOAuthNonStreaming(resp, ResponseSink{Writer: c.Writer}, svc.Output.ImageOptions(c), "b64_json", "gpt-image-2")
	require.Error(t, readErr)
	err := svc.handleOpenAIImagesOAuthResponseError(context.Background(), c, provider, "gpt-image-2", "https://api.openai.com/v1/responses", resp, OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c), readErr)

	var failoverErr *forwardcore.UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.JSONEq(t, `{"error":{"type":"upstream_error","code":"upstream_http2_stream_error","message":"Upstream HTTP/2 stream failed"}}`, string(failoverErr.ResponseBody))
	require.Equal(t, "req_h2_read_failure", http.Header(failoverErr.ResponseHeaders).Get("x-request-id"))
	require.Equal(t, "preserved", http.Header(failoverErr.ResponseHeaders).Get("x-upstream"))
	resp.Header.Set("X-Upstream", "mutated")
	require.Equal(t, "preserved", http.Header(failoverErr.ResponseHeaders).Get("x-upstream"))

	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*ops.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "failover", events[0].Kind)
	require.Equal(t, "req_h2_read_failure", events[0].UpstreamRequestID)
	require.Equal(t, "Upstream HTTP/2 stream failed", events[0].Message)
}

// TestOpenAIImagesOAuthBodyReadErrorsNotMisclassified 验证本地取消、超限和业务错误不会伪装成传输故障。
func TestOpenAIImagesOAuthBodyReadErrorsNotMisclassified(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "context canceled", err: context.Canceled},
		{name: "response too large", err: fmt.Errorf("%w: limit=1", httpclient.ErrResponseBodyTooLarge)},
		{name: "semantic error", err: &upstreamopenai.OpenAIImagesUpstreamError{StatusCode: http.StatusBadRequest, ErrorType: "invalid_request_error", Code: "invalid_value", Message: "bad image request"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
			err := tt.err
			if tt.name != "semantic error" && upstreamopenai.ShouldClassifyUpstreamStreamReadError(err, httpclient.ErrResponseBodyTooLarge, c.Request.Context()) {
				err = upstreamopenai.NewUpstreamStreamReadError(err)
			}

			got := newImagesFixture(imagesFixtureInputs{}).handleOpenAIImagesOAuthResponseError(context.Background(), c, &gatewayprovider.ExecutionProvider{Record: providerimages.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI}}, "gpt-image-2", "", resp, OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c), err)
			var failoverErr *forwardcore.UpstreamFailoverError
			require.False(t, errors.As(got, &failoverErr))
			require.ErrorIs(t, got, tt.err)
		})
	}
}

// TestOpenAIImagesOAuthTransportErrorAfterDownstreamWriteDoesNotFailover 验证下游开始输出后出现传输错误时停止换号。
func TestOpenAIImagesOAuthTransportErrorAfterDownstreamWriteDoesNotFailover(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	before := OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c)
	_, writeErr := c.Writer.Write([]byte("downstream image bytes"))
	require.NoError(t, writeErr)
	classifiedErr := upstreamopenai.NewUpstreamStreamReadError(errors.New("unexpected EOF"))
	provider := &gatewayprovider.ExecutionProvider{Record: providerimages.Record{LoadLocation: time.LoadLocation, ID: 5401, Name: "openai-oauth", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}}
	resp := &http.Response{Header: http.Header{"X-Request-Id": []string{"req_after_write"}}}

	err := newImagesFixture(imagesFixtureInputs{}).handleOpenAIImagesOAuthResponseError(context.Background(), c, provider, "gpt-image-2", "", resp, before, classifiedErr)

	var failoverErr *forwardcore.UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	require.ErrorIs(t, err, classifiedErr)
	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*ops.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "retry_exhausted_failover", events[0].Kind)
}

func TestBuildOpenAIImagesResponsesRequest_PassesThroughNForMultiImageModels(t *testing.T) {
	parsed := &media.ImageRequest{
		Endpoint: upstreamcore.OpenAIImagesGenerationsEndpoint,
		Model:    "gpt-image-2",
		Prompt:   "draw a cat",
		N:        2,
	}

	body, err := buildOpenAIImagesResponsesRequest(parsed, "gpt-image-2")
	require.NoError(t, err)
	require.NotNil(t, body)
	require.Equal(t, int64(2), gjson.GetBytes(body, "tools.0.n").Int())
	require.Equal(t, "gpt-image-2", gjson.GetBytes(body, "tools.0.model").String())
	require.Equal(t, "draw a cat", gjson.GetBytes(body, "input.0.content.0.text").String())
}

func TestBuildOpenAIImagesResponsesRequest_ForcesImageToolChoice(t *testing.T) {
	parsed := &media.ImageRequest{
		Endpoint: upstreamcore.OpenAIImagesGenerationsEndpoint,
		Model:    "gpt-image-2",
		Prompt:   "draw a cat",
	}

	body, err := buildOpenAIImagesResponsesRequest(parsed, "gpt-image-2")
	require.NoError(t, err)
	require.NotNil(t, body)
	require.Equal(t, "image_generation", gjson.GetBytes(body, "tool_choice.type").String())
	require.Equal(t, "image_generation", gjson.GetBytes(body, "tools.0.type").String())
	require.Equal(t, "gpt-image-2", gjson.GetBytes(body, "tools.0.model").String())
}

func TestBuildOpenAIImagesResponsesRequest_DoesNotPassNForDallE3(t *testing.T) {
	parsed := &media.ImageRequest{
		Endpoint: upstreamcore.OpenAIImagesGenerationsEndpoint,
		Model:    "dall-e-3",
		Prompt:   "draw a cat",
		N:        2,
	}

	body, err := buildOpenAIImagesResponsesRequest(parsed, "dall-e-3")
	require.NoError(t, err)
	require.NotNil(t, body)
	require.False(t, gjson.GetBytes(body, "tools.0.n").Exists())
	require.Equal(t, "dall-e-3", gjson.GetBytes(body, "tools.0.model").String())
}

func TestBuildOpenAIImagesResponsesRequest_StripsInputFidelity(t *testing.T) {
	parsed := &media.ImageRequest{
		Endpoint:      upstreamcore.OpenAIImagesEditsEndpoint,
		Model:         "gpt-image-2",
		Prompt:        "replace background",
		InputFidelity: "high",
		InputImageURLs: []string{
			"https://example.com/source.png",
		},
	}

	body, err := buildOpenAIImagesResponsesRequest(parsed, "gpt-image-2")
	require.NoError(t, err)
	require.NotNil(t, body)
	require.False(t, gjson.GetBytes(body, "tools.0.input_fidelity").Exists())
	require.Equal(t, "edit", gjson.GetBytes(body, "tools.0.action").String())
}

func TestBuildOpenAIImagesResponsesRequest_RequiresVerbatimUserPrompt(t *testing.T) {
	prompt := "画一个蓝色马克杯，杯身只写“SkelOT”，保持大小写；白色背景，不要增加其他文字。"
	parsed := &media.ImageRequest{
		Endpoint: upstreamcore.OpenAIImagesGenerationsEndpoint,
		Model:    "gpt-image-2",
		Prompt:   prompt,
		N:        1,
	}

	body, err := buildOpenAIImagesResponsesRequest(parsed, "gpt-image-2")
	require.NoError(t, err)
	require.Equal(t, upstreamopenai.ImagesVerbatimPromptInstructions, gjson.GetBytes(body, "instructions").String())
	require.Equal(t, prompt, gjson.GetBytes(body, "input.0.content.0.text").String())
}

func (r *countingModelRateLimitRepo) SetModelRateLimit(_ context.Context, _ int64, scope string, _ time.Time, _ ...string) error {
	r.calls++
	r.scopes = append(r.scopes, scope)
	return nil
}

func newImagesCooldownContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	return c, rec
}

func imagesCooldownProvider() *gatewayprovider.ExecutionProvider {
	return &gatewayprovider.ExecutionProvider{Record: providerimages.Record{LoadLocation: time.LoadLocation, ID: 77, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Name: "img-oauth"}}
}

func TestShouldCoolOpenAIImagesToolForError(t *testing.T) {
	cases := []struct {
		name string
		err  *upstreamopenai.OpenAIImagesUpstreamError
		want bool
	}{
		{
			name: "nil_error",
			err:  nil,
			want: false,
		},
		{
			// 网关从模型文字里推断出来的判据：只说明这一轮没出图。
			name: "synthesized_from_model_text",
			err: &upstreamopenai.OpenAIImagesUpstreamError{
				StatusCode:               http.StatusBadGateway,
				Code:                     "image_generation_unavailable",
				SynthesizedFromModelText: true,
			},
			want: false,
		},
		{
			// 上游自己在 error 帧里点名该状态：这才是提供商级证据，保持冷却。
			name: "structured_upstream_error_frame",
			err: &upstreamopenai.OpenAIImagesUpstreamError{
				StatusCode: http.StatusBadGateway,
				Code:       "image_generation_unavailable",
			},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, shouldCoolOpenAIImagesToolForError(tc.err))
		})
	}
}

// TestHandleOpenAIImagesOAuthResponseError_TextFallbackDoesNotCoolProvider 验证仅返回文字时提供商冷却状态保持原样。
func TestHandleOpenAIImagesOAuthResponseError_TextFallbackDoesNotCoolProvider(t *testing.T) {
	c, _ := newImagesCooldownContext(t)
	repo := &countingModelRateLimitRepo{}
	svc := newImagesFixture(imagesFixtureInputs{store: repo})
	provider := imagesCooldownProvider()

	upstreamErr := upstreamopenai.OpenAIImagesTextFallbackErrorForText("Here's a polished image prompt for your request.")
	require.NotNil(t, upstreamErr)
	require.Equal(t, "image_generation_unavailable", upstreamErr.Code)

	err := svc.handleOpenAIImagesOAuthResponseError(
		context.Background(), c, provider, "gpt-image-2", "https://upstream.example/v1/responses",
		&http.Response{StatusCode: http.StatusOK, Header: http.Header{}}, OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c), upstreamErr,
	)

	require.Zero(t, repo.calls, "模型闲聊不构成提供商级证据，不得写 30 分钟冷却")

	// 仅返回文字时仍触发换号。
	var failover *forwardcore.UpstreamFailoverError
	require.True(t, errors.As(err, &failover), "仍应触发换号，got %T", err)
}

// TestHandleOpenAIImagesOAuthResponseError_StructuredUnavailableStillCoolsProvider 验证对照不变式：上游 error 帧点名该状态时仍然冷却，否则等于把功能整个废掉。
func TestHandleOpenAIImagesOAuthResponseError_StructuredUnavailableStillCoolsProvider(t *testing.T) {
	c, _ := newImagesCooldownContext(t)
	repo := &countingModelRateLimitRepo{}
	svc := newImagesFixture(imagesFixtureInputs{store: repo})
	provider := imagesCooldownProvider()

	upstreamErr := &upstreamopenai.OpenAIImagesUpstreamError{
		StatusCode: http.StatusBadGateway,
		ErrorType:  "upstream_error",
		Code:       "image_generation_unavailable",
		Message:    "image generation tool is not available for this provider",
	}

	_ = svc.handleOpenAIImagesOAuthResponseError(
		context.Background(), c, provider, "gpt-image-2", "https://upstream.example/v1/responses",
		&http.Response{StatusCode: http.StatusOK, Header: http.Header{}}, OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c), upstreamErr,
	)

	require.Equal(t, 1, repo.calls, "结构化上游证据仍须写冷却")
	require.Equal(t, []string{providerimages.OpenAIImageGenerationRateLimitKey}, repo.scopes)
}

// TestOpenAIImagesTextFallback_MarksSynthesizedVerdicts 验证两个文字响应入口添加合成判定标记，违规拦截分支保持自身判定。
func TestOpenAIImagesTextFallback_MarksSynthesizedVerdicts(t *testing.T) {
	t.Run("plain_text_reply_is_synthesized", func(t *testing.T) {
		err := upstreamopenai.OpenAIImagesTextFallbackErrorForText("Here's a polished image prompt for your request.")
		require.NotNil(t, err)
		require.True(t, err.SynthesizedFromModelText)
		require.Equal(t, "image_generation_unavailable", err.Code)
		require.Equal(t, http.StatusBadGateway, err.StatusCode)
	})

	t.Run("body_entrypoint_is_synthesized", func(t *testing.T) {
		body := []byte("event: response.completed\n" +
			`data: {"type":"response.completed","response":{"id":"r","status":"completed",` +
			`"output":[{"type":"message","content":[{"type":"output_text","text":"I drafted a prompt for you."}]}]}}` +
			"\n\n")
		err := upstreamopenai.OpenAIImagesTextFallbackError(body)
		require.NotNil(t, err)
		require.True(t, err.SynthesizedFromModelText)
	})

	t.Run("content_policy_branch_unchanged", func(t *testing.T) {
		err := upstreamopenai.OpenAIImagesTextFallbackErrorForText("Blocked by our content policy.")
		require.NotNil(t, err)
		require.Equal(t, "content_policy_violation", err.Code)
		require.Equal(t, http.StatusBadRequest, err.StatusCode)
		// 该分支本来就不走冷却（Code 不匹配），标记与否都不改变行为；
		// 违规拦截结果的合成判定标记为 false。
		require.False(t, err.SynthesizedFromModelText)
	})

	t.Run("empty_text_yields_no_error", func(t *testing.T) {
		require.Nil(t, upstreamopenai.OpenAIImagesTextFallbackErrorForText("   "))
	})
}

// TestOpenAIImagesTextFallback_RemainsRetryableAndThusCascades 验证级联的前提条件：该错误确实是可重试的，所以会带着"已写冷却"的副作用换号。
// 此用例检查 502 的重试资格。
func TestOpenAIImagesTextFallback_RemainsRetryableAndThusCascades(t *testing.T) {
	err := upstreamopenai.OpenAIImagesTextFallbackErrorForText("Here's a polished image prompt for your request.")
	require.NotNil(t, err)
	require.True(t, upstreamopenai.IsOpenAIImagesRetryableUpstreamError(err),
		"文字兜底判据是可重试的——正因如此，写提供商冷却会沿号池级联")
}
