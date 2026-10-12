package httpapi

// WS 关闭归因场景覆盖 gateway/httpapi/ws_errors.go 和 gateway/ws/entry_rules.go，检查关闭分类与提供商故障上报的配合。

import (
	"context"
	"errors"
	"fmt"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	gatewayws "github.com/TokenFlux/TokenRouter/internal/gateway/ws"
)

// TestOpenAIWSIngressEndedByClient_GoingAwayWithoutCancellationStillReported 检查上游主动关闭时记录提供商故障。
func TestOpenAIWSIngressEndedByClient_GoingAwayWithoutCancellationStillReported(t *testing.T) {
	err := gatewayhttp.NewOpenAIWSClientCloseError(
		coderws.StatusGoingAway, "upstream going away", errors.New("upstream closed session"))

	require.False(t, gatewayhttp.ResponsesWSEndedByClient(err, gatewayhttp.ResponsesWSCloseInfo(err)))
	require.True(t, gatewayws.EntryShouldReportFailure(err), "真实上游故障仍须归因提供商")
}

// TestOpenAIWSIngressEndedByClient_AbnormalClosuresStillReportProviderFailure 检查异常关闭进入提供商故障判断。
func TestOpenAIWSIngressEndedByClient_AbnormalClosuresStillReportProviderFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "upstream_policy_violation",
			err: gatewayhttp.NewOpenAIWSClientCloseError(
				coderws.StatusPolicyViolation, "upstream websocket authentication failed",
				errors.New("upstream rejected credentials")),
		},
		{
			name: "upstream_internal_error",
			err: gatewayhttp.NewOpenAIWSClientCloseError(
				coderws.StatusInternalError, "upstream websocket proxy failed", nil),
		},
		{
			name: "bare_abnormal_closure",
			err:  coderws.CloseError{Code: coderws.StatusAbnormalClosure, Reason: "connection reset"},
		},
		{
			name: "generic_read_failure",
			err:  errors.New("upstream websocket read failed"),
		},
		{
			// 空闲超时之外的 deadline 按停滞故障处理。
			name: "deadline_without_normal_close",
			err:  fmt.Errorf("upstream stalled: %w", context.DeadlineExceeded),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.False(t, gatewayhttp.ResponsesWSEndedByClient(tc.err, gatewayhttp.ResponsesWSCloseInfo(tc.err)))
			require.True(t, gatewayws.EntryShouldReportFailure(tc.err))
		})
	}
}
