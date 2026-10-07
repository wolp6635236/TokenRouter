package grok_test

import (
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
	"github.com/stretchr/testify/require"
)

// TestResolveGrokStreamIdleTimeout 验证 Grok 流空闲超时的选择规则。
func TestResolveGrokStreamIdleTimeout(t *testing.T) {
	require.Equal(t, 90*time.Second, grok.ResolveStreamIdleTimeout(90))
	require.Equal(t, grok.DefaultStreamIdleTimeout, grok.ResolveStreamIdleTimeout(0))
	require.Equal(t, grok.DefaultStreamIdleTimeout, grok.ResolveStreamIdleTimeout(-1))
}
