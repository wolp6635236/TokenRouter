package httpapi

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
)

func TestOpenAIFirstOutputTimeoutForReasoningEffort(t *testing.T) {
	svc := newResponsesFixture(responsesFixtureInputs{options: &responsesFixtureOptions{Response: OpenAIResponseOptions{OpenAIFirstOutputTimeoutSeconds: 120, OpenAIHighEffortFirstOutputTimeoutSeconds: 300}}})

	require.Equal(t, 120*time.Second, svc.Output.FirstOutputTimeout("low"))
	require.Equal(t, 300*time.Second, svc.Output.FirstOutputTimeout("high"))
	require.Equal(t, 300*time.Second, svc.Output.FirstOutputTimeout("xhigh"))
	require.Equal(t, 300*time.Second, svc.Output.FirstOutputTimeout("max"))

	// 请求指定的 max 透传至第三方上游，首输出等待时间与其他高推理档位一致。
	effort := requeststate.ExtractOpenAIReasoningEffortFromBody(
		[]byte(`{"model":"deepseek-v4-flash","reasoning":{"effort":"max"}}`))
	require.NotNil(t, effort)
	require.Equal(t, 300*time.Second, svc.Output.FirstOutputTimeout(*effort))
}
