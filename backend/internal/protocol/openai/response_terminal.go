package openai

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// SupplementCompactionItemFromSSE 为 compact 终态补上事件流中的 compaction item。
// 终态 output 非空但缺少 compaction 时，从 output_item.done 或 added 取 raw JSON 补入。
// Codex remote compact v2 从 output_item.done 收集且要求恰好一个 compaction item，
// SSE 转 JSON 与流式透传需要提供相同结果。非 compact 请求原样返回。
func SupplementCompactionItemFromSSE(compact bool, finalResponse []byte, bodyText string) []byte {
	if !compact {
		return finalResponse
	}
	if len(gjson.GetBytes(finalResponse, "output").Array()) == 0 {
		// 空 output 由 reconstructResponseOutputFromSSE 重建。
		return finalResponse
	}
	if ResponsesOutputHasCompactionItem(finalResponse) {
		return finalResponse
	}
	item, found := FindRawCompactionItemFromSSE(bodyText)
	if !found {
		return finalResponse
	}
	patched, err := sjson.SetRawBytes(finalResponse, "output.-1", item)
	if err != nil {
		return finalResponse
	}
	return patched
}

func OpenAIStreamEventIsTerminal(data string) bool {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}
	if trimmed == "[DONE]" {
		return true
	}
	return OpenAIStreamEventTypeIsTerminal(gjson.Get(trimmed, "type").String())
}

// OpenAIStreamEventIsTerminalWithType 使用调用方已提取的 type 判断终态。
func OpenAIStreamEventIsTerminalWithType(data, eventType string) bool {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return false
	}
	if trimmed == "[DONE]" {
		return true
	}
	return OpenAIStreamEventTypeIsTerminal(eventType)
}

// GenericFailedEventPayload 返回客户端可识别的通用失败事件。
func GenericFailedEventPayload(_ ...[]byte) []byte {
	return []byte(`{"type":"response.failed","response":{"error":{"message":"Upstream gateway error"}}}`)
}
