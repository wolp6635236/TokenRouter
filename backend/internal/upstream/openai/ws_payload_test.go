package openai

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildReplayTurnPayload 构造第 turn 轮的全量重发 payload：客户端历史逐轮追加
// itemBytes 大小的项（Codex store=false 模式）。
func buildReplayTurnPayload(turn, itemBytes int) []byte {
	var b strings.Builder
	_, _ = b.WriteString(`{"type":"response.create","model":"gpt-5.5","stream":true,"input":[`)
	filler := strings.Repeat("x", itemBytes)
	for i := 1; i <= turn; i++ {
		if i > 1 {
			_, _ = b.WriteString(",")
		}
		_, _ = fmt.Fprintf(&b, `{"type":"input_text","text":"turn-%d-%s"}`, i, filler)
	}
	_, _ = b.WriteString(`]}`)
	return []byte(b.String())
}

// TestOpenAIWSReplayStateBuildAllocationBounded 检查 128 轮、每轮约 10KiB 增量并重发全量历史时的内存分配。
// 状态构建和历史保存共用报文正文，累计分配约为 85MB 正文加头数组及解析开销。
// 逐轮深拷贝会产生 O(T²) 的累计分配，本场景超过 160MB。
func TestOpenAIWSReplayStateBuildAllocationBounded(t *testing.T) {
	const (
		turns     = 128
		itemBytes = 10 * 1024
	)

	payloads := make([][]byte, 0, turns)
	for turn := 1; turn <= turns; turn++ {
		payloads = append(payloads, buildReplayTurnPayload(turn, itemBytes))
	}

	var history []json.RawMessage
	historyExists := false
	delta := []json.RawMessage{json.RawMessage(`{"type":"function_call","id":"item_1","call_id":"call_1","name":"exec","arguments":"{}"}`)}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for turn := 1; turn <= turns; turn++ {
		items, exists, err := BuildOpenAIWSReplayInputSequence(history, historyExists, payloads[turn-1], turn > 1)
		require.NoError(t, err)
		require.True(t, exists)
		require.Len(t, items, turn)
		// 保存历史 + collector 增量合并（与 ingress/bridge 保存点同构）。
		history = CombineOpenAIWSReplayItems(items, delta)
		historyExists = true
		// 下一轮 payload 含全部用户项但不含 collector 项，触发 sanitize+merge 路径中
		// 最常见的 prefix 分支比较。
		history = history[:len(history)-1]
	}

	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc

	// 正文共享实现全程约 2.5MB（头数组与 gjson 解析开销）。任何逐轮 O(H) 的
	// 字节级复制（extract 拷贝、正文 clone、保存点深拷贝）都会叠加 ≥84MB
	// （sum(t×10KiB)），超过测试设置的上限。
	const maxAllocatedBytes = 32 * 1024 * 1024
	require.Lessf(
		t,
		allocated,
		uint64(maxAllocatedBytes),
		"replay 状态构建累计分配 %d bytes 超出上界，疑似回归为逐轮深拷贝",
		allocated,
	)
}

func TestApplyOpenAIWSRetryPayloadStrategy_KeepPromptCacheKey(t *testing.T) {
	payload := map[string]any{
		"model":            "gpt-5.3-codex",
		"prompt_cache_key": "pcache_123",
		"include":          []any{"reasoning.encrypted_content"},
		"text": map[string]any{
			"verbosity": "low",
		},
		"tools": []any{map[string]any{"type": "function"}},
	}

	strategy, removed := ApplyWSRetryPayloadStrategy(payload, 3)
	require.Equal(t, "trim_optional_fields", strategy)
	require.Contains(t, removed, "include")
	require.NotContains(t, removed, "prompt_cache_key")
	require.Equal(t, "pcache_123", payload["prompt_cache_key"])
	require.NotContains(t, payload, "include")
	require.Contains(t, payload, "text")
}

func TestApplyOpenAIWSRetryPayloadStrategy_AttemptSixKeepsSemanticFields(t *testing.T) {
	payload := map[string]any{
		"prompt_cache_key":    "pcache_456",
		"instructions":        "long instructions",
		"tools":               []any{map[string]any{"type": "function"}},
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"include":             []any{"reasoning.encrypted_content"},
		"text":                map[string]any{"verbosity": "high"},
	}

	strategy, removed := ApplyWSRetryPayloadStrategy(payload, 6)
	require.Equal(t, "trim_optional_fields", strategy)
	require.Contains(t, removed, "include")
	require.NotContains(t, removed, "prompt_cache_key")
	require.Equal(t, "pcache_456", payload["prompt_cache_key"])
	require.Contains(t, payload, "instructions")
	require.Contains(t, payload, "tools")
	require.Contains(t, payload, "tool_choice")
	require.Contains(t, payload, "parallel_tool_calls")
	require.Contains(t, payload, "text")
}
