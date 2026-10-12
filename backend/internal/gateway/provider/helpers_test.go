package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/gateway/modeltrace"
)

// stubCredRepo 为凭据解析和影子提供商测试返回指定的母提供商。
// 通过嵌入接口补齐方法集，调用未实现的方法会 panic。
type stubCredRepo struct {
	ExecutionProviderStore

	parent *ExecutionProvider
}

func mustAPIKeyResponseModels(t *testing.T, ctx context.Context) []string {
	t.Helper()
	trace, ok := modeltrace.FromContext(ctx)
	require.True(t, ok)
	return trace.ResponseModels()
}

func (s *stubCredRepo) GetByID(_ context.Context, _ int64) (*ExecutionProvider, error) {
	return s.parent, nil
}

func newStubCredRepo(parent *ExecutionProvider) ExecutionProviderStore {
	return &stubCredRepo{parent: parent}
}
