package forward

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

func TestUpstreamResponseModelObserverServiceTier(t *testing.T) {
	t.Run("Responses 终止事件覆盖请求档位回显", func(t *testing.T) {
		observer := &ResponseObserver{}
		observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"gpt-5.6","service_tier":"priority"}}`), "response.created")
		require.Empty(t, observer.ServiceTier())

		observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-5.6","service_tier":"default"}}`), "response.completed")
		require.Equal(t, "default", observer.ServiceTier())
	})

	t.Run("Chat Completions 档位冲突时不采信", func(t *testing.T) {
		observer := &ResponseObserver{}
		observer.ObserveOpenAI([]byte(`{"model":"gpt-5.6","service_tier":"priority"}`), "")
		observer.ObserveOpenAI([]byte(`{"model":"gpt-5.6","service_tier":"default"}`), "")
		require.Empty(t, observer.ServiceTier())
	})

	t.Run("Chat Completions 终态档位覆盖早期回显", func(t *testing.T) {
		observer := &ResponseObserver{}
		observer.ObserveOpenAI([]byte(`{"model":"gpt-5.6","service_tier":"priority","choices":[{"finish_reason":""}]}`), "")
		terminal := []byte(`{"model":"gpt-5.6","service_tier":"default","choices":[{"finish_reason":"stop"}]}`)
		observer.ObserveOpenAI(terminal, openai.OpenAIChatCompletionServiceTierEventType(terminal))
		require.Equal(t, "default", observer.ServiceTier())
	})

	t.Run("Anthropic usage.speed 可作为实际档位", func(t *testing.T) {
		observer := &ResponseObserver{}
		observer.ObserveAnthropic([]byte(`{"type":"message_start","message":{"model":"claude-opus-5","usage":{"speed":"fast"}}}`))
		require.Equal(t, "fast", observer.ServiceTier())

		observer.ObserveAnthropic([]byte(`{"type":"message_delta","usage":{"speed":"standard"}}`))
		require.Empty(t, observer.ServiceTier())
	})

	t.Run("未知档位和空对象安全降级", func(t *testing.T) {
		observer := &ResponseObserver{}
		observer.ObserveOpenAI([]byte(`{"service_tier":"turbo"}`), "")
		require.Empty(t, observer.ServiceTier())
		var nilObserver *ResponseObserver
		require.Empty(t, nilObserver.ServiceTier())
	})
}

func TestUpstreamResponseModelObserver_ObservesServiceTier(t *testing.T) {
	t.Parallel()

	observer := &ResponseObserver{}
	// response.created 等有类型的非终止事件回显请求档位，实际处理档位从终态读取。
	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"gpt-5.5","service_tier":"flex"}}`), "response.created")
	require.Empty(t, observer.ServiceTier())

	// terminal 声明（带 model 帧）优先。
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-5.5","service_tier":"default"}}`), "response.completed")
	require.Equal(t, "default", observer.ServiceTier())

	// Chat Completions 顶层 service_tier 按 untyped payload 观察（无 type 字段）。
	ccObserver := &ResponseObserver{}
	ccObserver.ObserveOpenAI([]byte(`{"id":"chatcmpl-1","model":"gpt-5.5","service_tier":"priority","choices":[]}`), "")
	require.Equal(t, "priority", ccObserver.ServiceTier())

	// 无 model 的帧不触发 tier 观察（上游约束：tier 声明必带 model）。
	modelFree := &ResponseObserver{}
	modelFree.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"service_tier":"default"}}`), "response.completed")
	require.Empty(t, modelFree.ServiceTier())
}
