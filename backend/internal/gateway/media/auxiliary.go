package media

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/protocol"
)

// RealtimeAudioUsage 为已观测到音频且时长为正的会话计算用量。
func RealtimeAudioUsage(elapsed time.Duration, audioObserved bool) *protocol.AudioUsage {
	if !audioObserved || elapsed <= 0 {
		return nil
	}
	return &protocol.AudioUsage{Mode: "realtime", DurationOrUnits: elapsed.Minutes()}
}

// TTSInputText 按 input、text、prompt 顺序取首个字符串，包括空字符串。
func TTSInputText(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	for _, key := range []string{"input", "text", "prompt"} {
		if raw, ok := payload[key]; ok {
			// JSON null 不能当作字符串，从而保留后续字段回退。
			if len(raw) == 0 || raw[0] != '"' {
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) == nil {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

// AlphaEndpointUnsupported 只对 API Key 缺少独立搜索端点的响应允许换号。
func AlphaEndpointUnsupported(apiKey bool, statusCode int) bool {
	return apiKey && (statusCode == 404 || statusCode == 405)
}

// AlphaProviderErrorSideEffects 根据搜索端点错误决定是否更新提供商健康状态。
func AlphaProviderErrorSideEffects(statusCode int) bool {
	switch statusCode {
	case 401, 404, 405:
		return false
	default:
		return true
	}
}

// RequiredModel 从传入的报文字节中读取必填模型字段。
func RequiredModel(body []byte, trim bool) (string, bool) {
	value := gjson.GetBytes(body, "model")
	if !value.Exists() || value.Type != gjson.String || strings.TrimSpace(value.String()) == "" {
		return "", false
	}
	if trim {
		return strings.TrimSpace(value.String()), true
	}
	return value.String(), true
}

// IsVideoCreate 区分异步创建与查询，不能在创建成功时扣费。
func IsVideoCreate(endpoint string) bool {
	switch endpoint {
	case "videos_generations", "videos_edits", "videos_extensions":
		return true
	default:
		return false
	}
}

// RecordImmediateImages 只允许带有实际图片产出的即时生成完成事件。
func RecordImmediateImages(endpoint, requestModel string, imageCount int) bool {
	if strings.TrimSpace(requestModel) == "" || imageCount <= 0 {
		return false
	}
	return endpoint == "images_generations" || endpoint == "images_edits"
}

// RewriteMappedMediaBody 保留未命中映射时原字节与 multipart content-type。
func RewriteMappedMediaBody(body []byte, contentType string, mapped bool, model string, rewrite func([]byte, string, string) ([]byte, string, error)) ([]byte, string, error) {
	if !mapped || strings.TrimSpace(model) == "" {
		return body, contentType, nil
	}
	return rewrite(body, contentType, model)
}
