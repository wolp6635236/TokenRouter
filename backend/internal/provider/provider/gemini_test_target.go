package provider

import (
	"context"
	"net/http"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

type geminiTestTarget struct {
	executor *GeminiProviderTest
	record   *provider.Record
}

// Target 只公开测试资格，凭据留在本次平台执行句柄中。
func (s *GeminiProviderTest) Target(value *provider.Record) provider.TestTarget {
	return geminiTestTarget{executor: s, record: value}
}

func (t geminiTestTarget) Information() provider.TestTargetInfo {
	return provider.TestTargetInfo{ProviderSnapshot: t.record.RoutingSnapshot(), APIProtocol: (provider.ProtocolTarget{Record: t.record}).GetAPIProtocol()}
}

func (t geminiTestTarget) Execute(ctx context.Context, request provider.PreparedTestRequest, sink provider.TestEventSink) error {
	headers := make(http.Header)
	headers.Set("User-Agent", request.UserAgent)
	run := NewTestRun(ctx, headers, sink)
	defer run.Cancel()
	// 自动测试的 UA 保存在本次执行状态中。
	executor := *t.executor
	if request.Automatic {
		executor.UserAgent = request.UserAgent
	}
	return run.Result(executor.Execute(run, t.record, request.Model, request.Prompt, request.TestType))
}
