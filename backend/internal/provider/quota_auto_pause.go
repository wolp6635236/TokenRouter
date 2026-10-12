package provider

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const CodexAutoPauseStaleAfter = 2 * time.Hour

// QuotaAutoPauseSettings 提供动态阈值，缺省项由调用方使用回退值。
type QuotaAutoPauseSettings struct {
	DefaultThreshold5h float64 `json:"default_threshold_5h"`
	DefaultThreshold7d float64 `json:"default_threshold_7d"`
}

// QuotaAutoPauseDecision 仅表达窗口裁决，不写数据库或调度缓存。
type QuotaAutoPauseDecision struct {
	Window                 string
	Threshold, Utilization float64
}

func quotaClamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

// ResolveProviderExtraBool 从提供商 extra 中读取类 bool 值，并兼容 JSON 反序列化
// 可能产生的几种形态（bool、"true"/"false" 字符串、0/1 数字）。
func ResolveProviderExtraBool(extra map[string]any, key string) bool {
	if len(extra) == 0 {
		return false
	}
	value, ok := extra[key]
	if !ok || value == nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		return err == nil && parsed
	case float64:
		return v != 0
	case float32:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i != 0
		}
	}
	return false
}

func ResolveProviderExtraNumber(extra map[string]any, keys ...string) (float64, bool) {
	if len(extra) == 0 {
		return 0, false
	}
	for _, key := range keys {
		value, ok := extra[key]
		if !ok || value == nil {
			continue
		}
		switch v := value.(type) {
		case float64:
			return v, true
		case float32:
			return float64(v), true
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		case json.Number:
			parsed, err := v.Float64()
			if err == nil {
				return parsed, true
			}
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}

// ResolveOpenAIQuotaUtilization 返回有效窗口利用率，已重置或陈旧的快照按可用处理。
func ResolveOpenAIQuotaUtilization(extra map[string]any, window string, now time.Time) (float64, bool) {
	usedPercent := ReadOpenAIQuotaUsedPercent(extra, window)
	if usedPercent <= 0 {
		return 0, false
	}
	if OpenAIQuotaWindowReset(extra, window, now) {
		return 0, false
	}
	// 快照陈旧时允许请求通过，随后用响应头更新快照，
	// 提供商可重新取得当前窗口的用量（issue #2994）。
	if OpenAICodexSnapshotStaleForPause(extra, now) {
		return 0, false
	}
	return usedPercent / 100, true
}

// OpenAICodexSnapshotStaleForPause 按写入时间判断快照是否陈旧，时间缺失或无法解析时视为有效。
func OpenAICodexSnapshotStaleForPause(extra map[string]any, now time.Time) bool {
	if len(extra) == 0 {
		return false
	}
	updatedRaw, ok := extra["codex_usage_updated_at"]
	if !ok {
		return false
	}
	updatedAt, err := ParseUsageTime(fmt.Sprint(updatedRaw))
	if err != nil {
		return false
	}
	return now.Sub(updatedAt) >= CodexAutoPauseStaleAfter
}

// OpenAIQuotaWindowReset 优先绝对重置时间，再使用以快照写入时间为基准的相对秒数。
func OpenAIQuotaWindowReset(extra map[string]any, window string, now time.Time) bool {
	if len(extra) == 0 {
		return false
	}
	if resetAtRaw, ok := extra["codex_"+window+"_reset_at"]; ok {
		if resetAt, err := ParseUsageTime(fmt.Sprint(resetAtRaw)); err == nil {
			return !now.Before(resetAt)
		}
	}
	resetAfter := ParseExtraInt(extra["codex_"+window+"_reset_after_seconds"])
	if resetAfter <= 0 {
		return false
	}
	base := now
	if updatedRaw, ok := extra["codex_usage_updated_at"]; ok {
		if updatedAt, err := ParseUsageTime(fmt.Sprint(updatedRaw)); err == nil {
			base = updatedAt
		}
	}
	resetAt := base.Add(time.Duration(resetAfter) * time.Second)
	return !now.Before(resetAt)
}

func ReadOpenAIQuotaUsedPercent(extra map[string]any, window string) float64 {
	if len(extra) == 0 {
		return 0
	}
	if value, ok := ResolveProviderExtraNumber(extra, "codex_"+window+"_used_percent"); ok {
		return value
	}
	return 0
}

func ResolveQuotaAutoPauseThresholds(extra map[string]any, settings QuotaAutoPauseSettings) (float64, float64) {
	threshold5h, _ := ResolveProviderExtraNumber(extra, "auto_pause_5h_threshold")
	threshold7d, _ := ResolveProviderExtraNumber(extra, "auto_pause_7d_threshold")
	threshold5h = quotaClamp(threshold5h)
	threshold7d = quotaClamp(threshold7d)
	if threshold5h > 0 && threshold7d > 0 {
		return threshold5h, threshold7d
	}
	if threshold5h <= 0 {
		threshold5h = quotaClamp(settings.DefaultThreshold5h)
	}
	if threshold7d <= 0 {
		threshold7d = quotaClamp(settings.DefaultThreshold7d)
	}
	return threshold5h, threshold7d
}

func EvaluateQuotaAutoPause(platform string, extra map[string]any, settings QuotaAutoPauseSettings, now time.Time) (bool, QuotaAutoPauseDecision) {
	if platform != PlatformOpenAI {
		return false, QuotaAutoPauseDecision{}
	}
	// 提供商的禁用标记优先于全局默认值。提供商阈值留空时表示“使用全局默认”，
	// 一旦存在全局默认值，管理员就无法让单个提供商豁免自动暂停。
	// 禁用标记按窗口拆分，因此提供商可以只退出 5h 或只退出 7d 自动暂停。
	disabled5h := ResolveProviderExtraBool(extra, "auto_pause_5h_disabled")
	disabled7d := ResolveProviderExtraBool(extra, "auto_pause_7d_disabled")
	threshold5h, threshold7d := ResolveQuotaAutoPauseThresholds(extra, settings)
	if !disabled5h && threshold5h > 0 {
		if utilization, ok := ResolveOpenAIQuotaUtilization(extra, "5h", now); ok && utilization >= threshold5h {
			return true, QuotaAutoPauseDecision{Window: "5h", Threshold: threshold5h, Utilization: utilization}
		}
	}
	if !disabled7d && threshold7d > 0 {
		if utilization, ok := ResolveOpenAIQuotaUtilization(extra, "7d", now); ok && utilization >= threshold7d {
			return true, QuotaAutoPauseDecision{Window: "7d", Threshold: threshold7d, Utilization: utilization}
		}
	}
	return false, QuotaAutoPauseDecision{}
}
