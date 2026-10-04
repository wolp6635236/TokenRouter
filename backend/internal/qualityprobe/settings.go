package qualityprobe

import (
	"sort"
	"strings"
	"time"
)

const (
	// DefaultProbeModel 是未配置或目录没有 Astra 时使用的探测模型。
	DefaultProbeModel = "gpt-6-astra"
	// DefaultNotifyEmail 是三次探测失败后的默认收件人。
	DefaultNotifyEmail = "295783453@qq.com"
	// SettingKeyQualityProbe 是运行时设置键。
	SettingKeyQualityProbe = "quality_probe_settings"
	// PlatformOpenAI 是第一期覆盖的提供商平台。
	PlatformOpenAI = "openai"
	// LatestModelToken 表示从平台目录选择当前 Astra。
	LatestModelToken = "latest"
)

// Settings 是降智探测的运行配置。
type Settings struct {
	Enabled     bool
	Interval    time.Duration
	Model       string
	Cooldown    time.Duration
	MaxAttempts int
	NotifyEmail string
}

// DefaultSettings 返回关闭状态的默认配置。
func DefaultSettings() Settings {
	return Settings{
		Enabled:     false,
		Interval:    30 * time.Minute,
		Model:       DefaultProbeModel,
		Cooldown:    5 * time.Minute,
		MaxAttempts: 3,
		NotifyEmail: DefaultNotifyEmail,
	}
}

// ResolveProbeModel 选择本轮探测模型。configured 为空时用默认 Astra；
// 为 latest 时从 catalog 里取字典序最大的 Astra 名称。
func ResolveProbeModel(configured string, catalog []string) string {
	name := strings.TrimSpace(configured)
	if name == "" {
		return DefaultProbeModel
	}
	if !strings.EqualFold(name, LatestModelToken) {
		return name
	}
	astras := make([]string, 0, len(catalog))
	for _, item := range catalog {
		if isGPT6AstraModel(item) {
			astras = append(astras, item)
		}
	}
	if len(astras) == 0 {
		return DefaultProbeModel
	}
	sort.Strings(astras)
	return astras[len(astras)-1]
}

func isGPT6AstraModel(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	normalized = strings.TrimPrefix(normalized, "openai/")
	return normalized == DefaultProbeModel || strings.HasPrefix(normalized, DefaultProbeModel+"-")
}
