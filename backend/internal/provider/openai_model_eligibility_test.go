package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsOpenAIOAuthServableModel(t *testing.T) {
	require.True(t, IsOpenAIOAuthServableModel("gpt-5.4-high"))
	require.True(t, IsOpenAIOAuthServableModel("  gpt-5.3-codex  "))
	require.True(t, IsOpenAIOAuthServableModel("claude-3-5-haiku-20241022"))
	require.True(t, IsOpenAIOAuthServableModel("DeepThink-x"))  // 非黑名单前缀，保持允许。
	require.False(t, IsOpenAIOAuthServableModel("DeepSeek-V4")) // 大小写不敏感。
	require.False(t, IsOpenAIOAuthServableModel("qwen3-235b-thinking"))
	require.False(t, IsOpenAIOAuthServableModel("k3"))
	require.False(t, IsOpenAIOAuthServableModel("k3-256k"))
	require.False(t, IsOpenAIOAuthServableModel("provider/k3"))
	require.True(t, IsOpenAIOAuthServableModel("my-k3-alias"))   // 自定义别名继续 fail-open。
	require.True(t, IsOpenAIOAuthServableModel("deepseekcoder")) // 无连字符时不匹配黑名单前缀。
}
