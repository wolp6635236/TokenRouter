package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/protocol/wirejson"
)

// AdaptOpenAIResponsesClientTools 将 Codex 专用工具降级为上游可接受的 function 工具。
func AdaptOpenAIResponsesClientTools(body []byte) ([]byte, ResponsesClientToolMapping, error) {
	if !NeedsOpenAIResponsesClientToolAdaptation(body) {
		return body, ResponsesClientToolMapping{}, nil
	}
	return AdaptOpenAIResponsesClientToolsWithMapping(body, ResponsesClientToolMapping{})
}

// AdaptOpenAIResponsesClientToolsWithMapping 支持在 bridge 多轮请求中继承工具映射。
// body 在请求入口已解析，此处仍检查尾随 JSON，存在额外内容时返回错误。
func AdaptOpenAIResponsesClientToolsWithMapping(
	body []byte,
	inherited ResponsesClientToolMapping,
) ([]byte, ResponsesClientToolMapping, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var requestBody map[string]any
	if err := decoder.Decode(&requestBody); err != nil {
		return body, ResponsesClientToolMapping{}, fmt.Errorf("decode OpenAI Responses client tools: %w", err)
	}
	var trailingValue any
	if err := decoder.Decode(&trailingValue); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return body, ResponsesClientToolMapping{}, fmt.Errorf("decode OpenAI Responses client tools trailing data: %w", err)
	}
	mapping, changed, err := AdaptResponsesClientToolsWithInheritedMapping(requestBody, inherited)
	if err != nil || !changed {
		return body, mapping, err
	}
	rebuilt, err := wirejson.Marshal(requestBody)
	if err != nil {
		return body, ResponsesClientToolMapping{}, fmt.Errorf("encode OpenAI Responses client tools: %w", err)
	}
	return rebuilt, mapping, nil
}

func NeedsOpenAIResponsesClientToolAdaptation(body []byte) bool {
	needsAdaptation := false
	var visit func(gjson.Result) bool
	visit = func(value gjson.Result) bool {
		if value.IsObject() {
			switch strings.TrimSpace(value.Get("type").String()) {
			case "custom", "custom_tool_call", "custom_tool_call_output",
				"tool_search", "tool_search_call", "tool_search_output":
				needsAdaptation = true
				return false
			}
		}
		if value.IsObject() || value.IsArray() {
			value.ForEach(func(_, child gjson.Result) bool {
				return visit(child)
			})
		}
		return !needsAdaptation
	}
	visit(gjson.ParseBytes(body))
	return needsAdaptation
}

func IsOpenAIResponsesToolCallItemType(itemType string) bool {
	return openAIResponsesToolCallItemTypes[strings.ToLower(strings.TrimSpace(itemType))]
}
