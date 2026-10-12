package gemini

import (
	"encoding/json"
)

// CleanNativeThoughtSignatures 将 Gemini 请求中的 thoughtSignature 替换为给定的 dummy 签名。
// 粘性会话切换提供商后，此前提供商的签名会使新提供商校验失败，dummy 签名用于跳过该校验。
func CleanNativeThoughtSignatures(body []byte, placeholder string) []byte {
	if len(body) == 0 {
		return body
	}

	// 解析 JSON
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		// 如果解析失败，返回原始 body（可能不是 JSON 或格式不正确）
		return body
	}

	// 递归替换 thoughtSignature 为 dummy 签名
	replaced := replaceThoughtSignaturesRecursive(data, placeholder)

	// 重新序列化
	result, err := json.Marshal(replaced)
	if err != nil {
		// 如果序列化失败，返回原始 body
		return body
	}

	return result
}

// replaceThoughtSignaturesRecursive 递归遍历数据结构，将所有 thoughtSignature 字段替换为 dummy 签名。
func replaceThoughtSignaturesRecursive(data any, placeholder string) any {
	switch v := data.(type) {
	case map[string]any:
		// 创建新的 map，替换 thoughtSignature 为 dummy 签名
		result := make(map[string]any, len(v))
		for key, value := range v {
			// 替换 thoughtSignature 字段为 dummy 签名
			if key == "thoughtSignature" {
				result[key] = placeholder
				continue
			}
			// 递归处理嵌套结构
			result[key] = replaceThoughtSignaturesRecursive(value, placeholder)
		}
		return result

	case []any:
		// 递归处理数组中的每个元素
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = replaceThoughtSignaturesRecursive(item, placeholder)
		}
		return result

	default:
		// 基本类型（string, number, bool, null）直接返回
		return v
	}
}
