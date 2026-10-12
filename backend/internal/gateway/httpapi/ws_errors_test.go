package httpapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// TestOpenAIWSIngressEndedByClient_BareNormalClosureIsNotProviderFailure 检查关闭码、客户端结束原因与诊断结果。
func TestOpenAIWSIngressEndedByClient_BareNormalClosureIsNotProviderFailure(t *testing.T) {
	err := coderws.CloseError{Code: coderws.StatusNormalClosure, Reason: "client done"}

	// 裸 CloseError 与 OpenAIWSClientCloseError 的类型不同，关闭码都为 1000。
	var closeErr *OpenAIWSClientCloseError
	require.False(t, errors.As(err, &closeErr),
		"裸 coderws.CloseError 不是 *OpenAIWSClientCloseError，旧的 errors.As 必然为假")
	// 而按关闭码读，它确实是 1000。
	require.Equal(t, coderws.StatusNormalClosure, coderws.CloseStatus(err))

	require.True(t, ResponsesWSEndedByClient(err, ResponsesWSCloseInfo(err)))
}

// TestOpenAIWSIngressEndedByClient_WrappedBareNormalClosureIsNotProviderFailure 检查关闭码、客户端结束原因与诊断结果。
func TestOpenAIWSIngressEndedByClient_WrappedBareNormalClosureIsNotProviderFailure(t *testing.T) {
	err := fmt.Errorf("ingress turn 3: %w",
		coderws.CloseError{Code: coderws.StatusNormalClosure, Reason: "client done"})

	require.True(t, ResponsesWSEndedByClient(err, ResponsesWSCloseInfo(err)))
}

// TestOpenAIWSIngressEndedByClient_ClientCancelDuringTurnIsNotProviderFailure 检查关闭码、客户端结束原因与诊断结果。
func TestOpenAIWSIngressEndedByClient_ClientCancelDuringTurnIsNotProviderFailure(t *testing.T) {
	err := NewOpenAIWSClientCloseError(
		coderws.StatusGoingAway, "websocket request canceled", context.Canceled)

	// 客户端取消使用 1001 关闭码，并携带 context.Canceled。
	var closeErr *OpenAIWSClientCloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusGoingAway, closeErr.StatusCode())
	require.NotEqual(t, coderws.StatusNormalClosure, closeErr.StatusCode())

	require.True(t, ResponsesWSEndedByClient(err, ResponsesWSCloseInfo(err)))
}

// TestOpenAIWSIngressEndedByClient_GatewayNormalClosureStillRecognised 检查关闭码、客户端结束原因与诊断结果。
func TestOpenAIWSIngressEndedByClient_GatewayNormalClosureStillRecognised(t *testing.T) {
	err := NewOpenAIWSClientCloseError(
		coderws.StatusNormalClosure, "websocket idle timeout", context.DeadlineExceeded)

	require.True(t, ResponsesWSEndedByClient(err, ResponsesWSCloseInfo(err)))
}
