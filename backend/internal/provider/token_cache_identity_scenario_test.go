package provider

// 本文件检查 openai_token.go 与 claude_token.go 的缓存键平台隔离。

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheKeyUniqueness(t *testing.T) {
	// 不同平台使用各自的缓存键前缀。
	provider := &Record{ID: 123}

	openaiKey := OpenAITokenCacheKey(provider)
	claudeKey := ClaudeTokenCacheKey(provider)

	require.NotEqual(t, openaiKey, claudeKey, "OpenAI and Claude cache keys should be different")
	require.Contains(t, openaiKey, "openai:")
	require.Contains(t, claudeKey, "claude:")
}
