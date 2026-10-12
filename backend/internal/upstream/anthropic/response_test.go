package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/TokenFlux/TokenRouter/internal/protocol/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

func TestForceCacheBilling_TokenConversion(t *testing.T) {
	tests := []struct {
		name                    string
		forceCacheBilling       bool
		inputTokens             int
		cacheReadInputTokens    int
		expectedInputTokens     int
		expectedCacheReadTokens int
	}{
		{
			name:                    "force cache billing converts input to cache_read",
			forceCacheBilling:       true,
			inputTokens:             1000,
			cacheReadInputTokens:    500,
			expectedInputTokens:     0,
			expectedCacheReadTokens: 1500, // 500 + 1000
		},
		{
			name:                    "no force cache billing keeps tokens unchanged",
			forceCacheBilling:       false,
			inputTokens:             1000,
			cacheReadInputTokens:    500,
			expectedInputTokens:     1000,
			expectedCacheReadTokens: 500,
		},
		{
			name:                    "force cache billing with zero input tokens does nothing",
			forceCacheBilling:       true,
			inputTokens:             0,
			cacheReadInputTokens:    500,
			expectedInputTokens:     0,
			expectedCacheReadTokens: 500,
		},
		{
			name:                    "force cache billing with zero cache_read tokens",
			forceCacheBilling:       true,
			inputTokens:             1000,
			cacheReadInputTokens:    0,
			expectedInputTokens:     0,
			expectedCacheReadTokens: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 根据输入用量构造待转换的响应报文。
			usage := upstream.TokenUsage{
				InputTokens:          tt.inputTokens,
				CacheReadInputTokens: tt.cacheReadInputTokens,
			}

			// ParseClaudeUsageFromResponseBody 读取转换后的响应用量。
			body, err := json.Marshal(struct {
				Usage upstream.TokenUsage `json:"usage"`
			}{Usage: usage})
			if err != nil {
				t.Fatal(err)
			}
			if tt.forceCacheBilling && usage.InputTokens > 0 {
				body, err = ClassifyResponseInputAsCacheRead(body, &usage)
				if err != nil {
					t.Fatal(err)
				}
			}
			usage = *anthropic.ParseClaudeUsageFromResponseBody(body)

			if usage.InputTokens != tt.expectedInputTokens {
				t.Errorf("InputTokens = %d, want %d", usage.InputTokens, tt.expectedInputTokens)
			}
			if usage.CacheReadInputTokens != tt.expectedCacheReadTokens {
				t.Errorf("CacheReadInputTokens = %d, want %d", usage.CacheReadInputTokens, tt.expectedCacheReadTokens)
			}
		})
	}
}

// 非流式响应 reconcile 测试

func TestNonStreamingReconcile_KimiResponse(t *testing.T) {
	// 模拟 Kimi 非流式响应
	body := []byte(`{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "hello"}],
		"model": "kimi",
		"usage": {
			"input_tokens": 23,
			"output_tokens": 7,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens": 0,
			"cached_tokens": 23,
			"prompt_tokens": 23,
			"completion_tokens": 7
		}
	}`)

	// 模拟 handleNonStreamingResponse 中的逻辑
	var response struct {
		Usage upstream.TokenUsage `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(body, &response))

	// reconcile
	if response.Usage.CacheReadInputTokens == 0 {
		cachedTokens := gjson.GetBytes(body, "usage.cached_tokens").Int()
		if cachedTokens > 0 {
			response.Usage.CacheReadInputTokens = int(cachedTokens)
			if newBody, err := sjson.SetBytes(body, "usage.cache_read_input_tokens", cachedTokens); err == nil {
				body = newBody
			}
		}
	}

	// 验证内部 usage（计费用）
	assert.Equal(t, 23, response.Usage.CacheReadInputTokens)
	assert.Equal(t, 23, response.Usage.InputTokens)
	assert.Equal(t, 7, response.Usage.OutputTokens)

	// 验证返回给客户端的 JSON body
	assert.Equal(t, int64(23), gjson.GetBytes(body, "usage.cache_read_input_tokens").Int())
}

func TestNonStreamingReconcile_NativeClaude(t *testing.T) {
	// 原生 Claude 响应：cache_read_input_tokens 已有值
	body := []byte(`{
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_creation_input_tokens": 20,
			"cache_read_input_tokens": 30
		}
	}`)

	var response struct {
		Usage upstream.TokenUsage `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(body, &response))

	// CacheReadInputTokens == 30，条件不成立，整个 reconcile 分支不会执行
	assert.NotZero(t, response.Usage.CacheReadInputTokens)
	assert.Equal(t, 30, response.Usage.CacheReadInputTokens)
}

func TestNonStreamingReconcile_NoCachedTokens(t *testing.T) {
	// 没有 cached_tokens 字段
	body := []byte(`{
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens": 0
		}
	}`)

	var response struct {
		Usage upstream.TokenUsage `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(body, &response))

	if response.Usage.CacheReadInputTokens == 0 {
		cachedTokens := gjson.GetBytes(body, "usage.cached_tokens").Int()
		if cachedTokens > 0 {
			response.Usage.CacheReadInputTokens = int(cachedTokens)
			if newBody, err := sjson.SetBytes(body, "usage.cache_read_input_tokens", cachedTokens); err == nil {
				body = newBody
			}
		}
	}

	// cache_read_input_tokens 应保持为 0
	assert.Equal(t, 0, response.Usage.CacheReadInputTokens)
	assert.Equal(t, int64(0), gjson.GetBytes(body, "usage.cache_read_input_tokens").Int())
}
