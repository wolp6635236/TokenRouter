package capability

import (
	"strconv"
	"strings"
)

// LastOpenAIModelSegment 为能力分类读取路径尾段，不参与模型改写或价格候选。
func LastOpenAIModelSegment(model string) string {
	model = strings.TrimSpace(model)
	if index := strings.LastIndexByte(model, '/'); index >= 0 {
		return strings.TrimSpace(model[index+1:])
	}
	return model
}

// CanonicalizeOpenAIModelAliasSpelling 规范查询大小写，保留型号拼写及供应商前缀。
func CanonicalizeOpenAIModelAliasSpelling(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func OpenAIModelSupportsReasoningEffort(model string, effort string) bool {
	value := strings.ToLower(strings.TrimSpace(effort))
	if value == "" {
		return false
	}
	value = strings.NewReplacer("-", "", "_", "", " ", "").Replace(value)
	switch value {
	case "max":
		return OpenAIModelSupportsMaxReasoningEffort(model)
	case "ultra":
		// Ultra 不是上游 reasoning effort，任何模型都不应声明支持。
		return false
	default:
		return true
	}
}

func OpenAIModelSupportsMaxReasoningEffort(model string) bool {
	if IsOpenAIModelAtLeastVersion(model, 5, 6) {
		return true
	}

	// 国产模型的原生 max 档位与 GPT-5.6 使用同一 usage 语义。
	normalized := strings.ToLower(LastOpenAIModelSegment(model))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch {
	case strings.HasPrefix(normalized, "deepseek-v4"):
		return true
	case strings.HasPrefix(normalized, "glm-"):
		return true
	case strings.HasPrefix(normalized, "kimi-"), strings.HasPrefix(normalized, "moonshot-"):
		return true
	case normalized == "k3" || strings.HasPrefix(normalized, "k3-"):
		return true
	default:
		return false
	}
}

func IsOpenAIModelAtLeastVersion(model string, minMajor, minMinor int) bool {
	major, minor, ok := ParseOpenAIModelVersion(model)
	if !ok {
		return false
	}
	if major != minMajor {
		return major > minMajor
	}
	return minor >= minMinor
}

func ParseOpenAIModelVersion(model string) (major int, minor int, ok bool) {
	// 能力只读模型尾段，供应商前缀仍保留在转发和查价使用的完整 ID 中。
	normalized := strings.ToLower(LastOpenAIModelSegment(model))
	if normalized == "" || !strings.HasPrefix(normalized, "gpt-") {
		return 0, 0, false
	}

	rest := strings.TrimPrefix(normalized, "gpt-")
	majorEnd := 0
	for majorEnd < len(rest) && rest[majorEnd] >= '0' && rest[majorEnd] <= '9' {
		majorEnd++
	}
	if majorEnd == 0 {
		return 0, 0, false
	}

	major, err := strconv.Atoi(rest[:majorEnd])
	if err != nil {
		return 0, 0, false
	}

	minor = 0
	if majorEnd < len(rest) && rest[majorEnd] == '.' {
		minorStart := majorEnd + 1
		minorEnd := minorStart
		for minorEnd < len(rest) && rest[minorEnd] >= '0' && rest[minorEnd] <= '9' {
			minorEnd++
		}
		if minorEnd == minorStart {
			return 0, 0, false
		}
		minor, err = strconv.Atoi(rest[minorStart:minorEnd])
		if err != nil {
			return 0, 0, false
		}
	}

	return major, minor, true
}
