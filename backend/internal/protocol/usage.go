package protocol

// AudioUsage 记录音频处理模式及计费用量。
type AudioUsage struct {
	Mode            string  // realtime | tts | stt
	DurationOrUnits float64 // minutes / million-chars / hours
}

// TokenUsage 保存输入、输出和缓存 token 用量。
type TokenUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreation5mTokens    int // 5分钟缓存创建token（来自嵌套 cache_creation 对象）
	CacheCreation1hTokens    int // 1小时缓存创建token（来自嵌套 cache_creation 对象）
	ImageOutputTokens        int `json:"image_output_tokens,omitempty"`
	// Speed 记录 Claude 实际返回的处理速度，"fast" 会映射到内部 priority 计费。
	Speed string `json:"speed,omitempty"`
}

// ApplyCacheTTLOverride 将缓存创建用量归入指定的 TTL 类型。
// target 为“1h”时归入 1h，其余值归入 5m。
// TTL 分类发生变化时返回 true，用聚合值补齐 5m 明细时返回 false。
func ApplyCacheTTLOverride(usage *TokenUsage, target string) bool {
	// 兼容回退： 如果只有聚合字段但无 5m/1h 明细，将聚合字段归入 5m 默认类别
	if usage.CacheCreation5mTokens == 0 && usage.CacheCreation1hTokens == 0 && usage.CacheCreationInputTokens > 0 {
		usage.CacheCreation5mTokens = usage.CacheCreationInputTokens
	}

	total := usage.CacheCreation5mTokens + usage.CacheCreation1hTokens
	if total == 0 {
		return false
	}
	switch target {
	case "1h":
		if usage.CacheCreation1hTokens == total {
			return false // 已经全是 1h
		}
		usage.CacheCreation1hTokens = total
		usage.CacheCreation5mTokens = 0
	default: // "5m"
		if usage.CacheCreation5mTokens == total {
			return false // 已经全是 5m
		}
		usage.CacheCreation5mTokens = total
		usage.CacheCreation1hTokens = 0
	}
	return true
}

// IncludeIndependentReasoningTokens 仅在 total_tokens 证明推理 token 尚未计入输出时，
// 将独立的 reasoning token 加入计费输出。
// xAI Chat Completions 示例为 prompt=32、completion=9、reasoning=94、total=135；
// Responses 示例为 input=32、output=9、reasoning=110、total=151。OpenAI 标准
// completion_tokens 已包含推理 token，且 total 等于 input+output。
func IncludeIndependentReasoningTokens(input, output, total, reasoning int64) int64 {
	if input < 0 || output < 0 || reasoning <= 0 || total <= 0 {
		return output
	}
	if total == input+output {
		return output
	}
	gap := total - input - output
	if gap <= 0 {
		return output
	}
	if reasoning < gap {
		gap = reasoning
	}
	return output + gap
}

// HasObservedTokens 判断是否观测到正数 token 用量。
func (u *TokenUsage) HasObservedTokens() bool {
	if u == nil {
		return false
	}
	return u.InputTokens > 0 || u.OutputTokens > 0 ||
		u.CacheCreationInputTokens > 0 || u.CacheReadInputTokens > 0 ||
		u.CacheCreation5mTokens > 0 || u.CacheCreation1hTokens > 0 ||
		u.ImageOutputTokens > 0
}
