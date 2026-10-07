package requeststate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSingleProviderRetry_True(t *testing.T) {
	ctx := WithSingleProviderRetry(context.Background(), true)
	value, _ := SingleProviderRetryFromContext(ctx)
	require.True(t, value)
}

func TestIsSingleProviderRetry_False_NoValue(t *testing.T) {
	value, _ := SingleProviderRetryFromContext(context.Background())
	require.False(t, value)
}

func TestIsSingleProviderRetry_False_ExplicitFalse(t *testing.T) {
	ctx := WithSingleProviderRetry(context.Background(), false)
	value, _ := SingleProviderRetryFromContext(ctx)
	require.False(t, value)
}

func TestIsSingleProviderRetry_False_WrongType(t *testing.T) {
	// 任意旧式上下文值不能成为原生执行参数；合法输入必须是 bool。
	type unrelatedKey struct{}
	ctx := context.WithValue(context.Background(), unrelatedKey{}, "true")
	value, _ := SingleProviderRetryFromContext(ctx)
	require.False(t, value)
}
