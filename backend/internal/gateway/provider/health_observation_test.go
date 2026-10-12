package provider

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
)

// TestUpdateSessionWindow_EmptyHeadersSkipsOptionalDependencies 验证空响应头必须先短路；未装配健康服务的请求仍可完成正常输出与会话释放。
func TestUpdateSessionWindow_EmptyHeadersSkipsOptionalDependencies(t *testing.T) {
	var service *provideradapter.UpstreamHealth

	for name, headers := range map[string]http.Header{"nil": nil, "empty": {}, "unrelated": {"Content-Type": {"application/json"}}} {
		t.Run(name, func(t *testing.T) {
			require.NotPanics(t, func() {
				ObserveExecutionSessionWindow(context.Background(), service, nil,

					headers)
			})
		})
	}
}

// TestHealthObservationKeepsRawAndEffectiveModel 检查空模型与缺省模型的区别，供应商解析使用原始模型，冷却使用规范模型。
func TestHealthObservationKeepsRawAndEffectiveModel(t *testing.T) {
	ctx := requeststate.WithHealthModel(context.Background(), []string{"stored-model"})
	ctx = requeststate.WithOpenAIImagesEndpoint(requeststate.WithThinkingEnabled(ctx, false))
	for _, models := range [][]string{nil, {""}, {" mapped-model "}} {
		input := HealthObservationFromContext(ctx, http.StatusNotFound, nil, []byte("failure"), models)
		require.Equal(t, len(models) > 0, input.ModelProvided)
		require.Equal(t, requeststate.HealthModel(ctx, models), input.EffectiveModel)
		if len(models) > 0 {
			require.Equal(t, models[0], input.Model)
		}
		require.True(t, input.ImagesEndpoint)
		require.NotNil(t, input.Thinking)
		require.False(t, *input.Thinking)
	}
}
