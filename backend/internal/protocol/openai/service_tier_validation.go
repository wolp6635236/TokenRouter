package openai

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	invalidOpenAIServiceTierValueMaxLen = 64

	// ServiceTierPriority 表示上游报文中的优先服务档位。
	ServiceTierPriority  = "priority"
	ServiceTierUltrafast = "ultrafast"
	ServiceTierFlex      = "flex"
)

// InvalidServiceTierError 表示请求的 service_tier 无效，handler 将其转换为 400 invalid_request_error。
type InvalidServiceTierError struct {
	Value string
}

func NormalizeServiceTier(raw string) *string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return nil
	}
	if value == "fast" {
		value = "priority"
	}
	// 放过 OpenAI 官方文档定义的合法 tier 值，以及 Codex/API 新增的 ultrafast。
	// Codex 客户端会发 priority、flex 或 ultrafast；直连 OpenAI SDK 的用户还会
	// 透传 auto/default/scale。真未知值仍返回 nil，由
	// normalizeResponsesBodyServiceTier 从 body 中删除。
	switch value {
	case "priority", "flex", "auto", "default", "scale", ServiceTierUltrafast:
		return &value
	default:
		return nil
	}
}

func (e *InvalidServiceTierError) Error() string {
	return fmt.Sprintf("invalid service_tier %q: must be one of auto, default, fast, flex, priority, scale, ultrafast", e.Value)
}

func boundInvalidOpenAIServiceTierValue(raw string) string {
	if len(raw) <= invalidOpenAIServiceTierValueMaxLen {
		return raw
	}
	return raw[:invalidOpenAIServiceTierValueMaxLen] + "..."
}

// ValidateServiceTierField 校验 OpenAI 兼容请求体中的 service_tier 字段。
//
// 空值或 null 保持兼容；fast 归一化为 priority；priority、flex、auto、default、
// scale、ultrafast 原样通过。非字符串、空字符串或未知值返回校验错误。
func ValidateServiceTierField(body []byte) (string, error) {
	tierResult := gjson.GetBytes(body, "service_tier")
	if !tierResult.Exists() || tierResult.Type == gjson.Null {
		return "", nil
	}
	if tierResult.Type != gjson.String {
		return "", &InvalidServiceTierError{Value: "<non-string>"}
	}
	raw := strings.TrimSpace(tierResult.String())
	if raw == "" {
		return "", &InvalidServiceTierError{Value: raw}
	}
	norm := ServiceTierValue(raw)
	if norm == "" {
		return "", &InvalidServiceTierError{Value: boundInvalidOpenAIServiceTierValue(raw)}
	}
	return norm, nil
}

// ServiceTierValue 返回规范化后的服务档位，输入不合法时返回空字符串。
func ServiceTierValue(raw string) string {
	normalized := NormalizeServiceTier(raw)
	if normalized == nil {
		return ""
	}
	return *normalized
}
