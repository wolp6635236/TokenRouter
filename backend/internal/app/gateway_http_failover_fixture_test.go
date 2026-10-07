package app

import (
	"context"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
)

// mockTempUnscheduler 记录 TempUnscheduleRetryableError 的调用信息。
type mockTempUnscheduler struct {
	calls []tempUnscheduleCall
}

type tempUnscheduleCall struct {
	providerID  int64
	failoverErr *forwardcore.UpstreamFailoverError
}

func (m *mockTempUnscheduler) TempUnscheduleRetryableError(_ context.Context, providerID int64, failoverErr *forwardcore.UpstreamFailoverError) {
	m.calls = append(m.calls, tempUnscheduleCall{providerID: providerID, failoverErr: failoverErr})
}
