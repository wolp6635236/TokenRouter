package searchtools

import (
	"bytes"
	"encoding/json"
	"strings"
	"unsafe"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	wire "github.com/TokenFlux/TokenRouter/internal/protocol/anthropic"
)

const (
	blockTypeServerToolUse       = "server_tool_use"
	blockTypeWebSearchToolResult = "web_search_tool_result"
)

// 两种块类型只会作为带引号的 JSON 字符串值出现，因此可用原始子串做快速预检，
// 不受键值空格格式影响。
var (
	patternServerToolUse       = []byte(`"server_tool_use"`)
	patternWebSearchToolResult = []byte(`"web_search_tool_result"`)
)

// FilterWebSearchHistoryBlocks 从历史消息移除目标上游拒绝的搜索块。
// tool-use ID 带 ToolUseIDPrefix 的 server_tool_use 和 web_search_tool_result 由网关模拟，转发时全部移除。
// 要求思考内容回传的 DeepSeek、Kimi、GLM 等兼容上游接受 text、thinking、image、tool_use 和 tool_result，
// 因此移除全部搜索块；Anthropic strict 和未知协议族保留上游签发的搜索块。
// 模拟响应的文本摘要继续保留搜索上下文，剥空的消息按 FilterThinkingBlocksForRetry 的方式补占位文本。
// 无需修改时返回原 body。
func FilterWebSearchHistoryBlocks(body []byte, stripAll bool) []byte {
	if !bytes.Contains(body, patternServerToolUse) && !bytes.Contains(body, patternWebSearchToolResult) {
		return body
	}

	jsonStr := *(*string)(unsafe.Pointer(&body))
	msgsRes := gjson.Get(jsonStr, "messages")
	if !msgsRes.Exists() || !msgsRes.IsArray() {
		return body
	}

	var messages []any
	if err := json.Unmarshal(wire.SliceRawFromBody(body, msgsRes), &messages); err != nil {
		return body
	}

	modified := false
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msgMap["content"].([]any)
		if !ok {
			continue
		}

		// 延迟分配：只有命中需剥离的块才构建新 slice。
		var newContent []any
		for i, block := range content {
			blockMap, isMap := block.(map[string]any)
			if isMap && shouldStripWebSearchBlock(blockMap, stripAll) {
				if newContent == nil {
					newContent = make([]any, 0, len(content))
					newContent = append(newContent, content[:i]...)
				}
				continue
			}
			if newContent != nil {
				newContent = append(newContent, block)
			}
		}
		if newContent == nil {
			continue
		}
		modified = true
		if len(newContent) == 0 {
			role, _ := msgMap["role"].(string)
			placeholder := "(content removed)"
			if role == "assistant" {
				placeholder = "(assistant content removed)"
			}
			newContent = []any{map[string]any{"type": "text", "text": placeholder}}
		}
		msgMap["content"] = newContent
	}

	if !modified {
		return body
	}

	msgsBytes, err := json.Marshal(messages)
	if err != nil {
		return body
	}
	out, err := sjson.SetRawBytes(body, "messages", msgsBytes)
	if err != nil {
		return body
	}
	return out
}

func shouldStripWebSearchBlock(block map[string]any, stripAll bool) bool {
	blockType, _ := block["type"].(string)
	switch blockType {
	case blockTypeServerToolUse:
		if stripAll {
			return true
		}
		id, _ := block["id"].(string)
		return strings.HasPrefix(id, ToolUseIDPrefix)
	case blockTypeWebSearchToolResult:
		if stripAll {
			return true
		}
		id, _ := block["tool_use_id"].(string)
		return strings.HasPrefix(id, ToolUseIDPrefix)
	default:
		return false
	}
}
