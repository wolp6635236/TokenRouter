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
	// GroupIDs 限制自动探测覆盖的分组。空切片表示全部启用中的 OpenAI 分组。
	GroupIDs []int64
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
		GroupIDs:    nil,
	}
}

// NormalizeGroupIDs 去掉空值和重复，并按 ID 排序。
func NormalizeGroupIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}

// AutoGroupAllowed 判断自动探测是否覆盖该提供商。selected 为空且 active 为 nil 时，账号所属分组都视为启用。
func AutoGroupAllowed(selected, providerGroups []int64) bool {
	return AutoScopeAllowed(selected, providerGroups, nil)
}

// AutoScopeAllowed 要求账号至少属于一个启用中的分组；selected 非空时还要落在所选分组里。
func AutoScopeAllowed(selected, providerGroups []int64, active map[int64]struct{}) bool {
	selectedSet := map[int64]struct{}{}
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	for _, id := range providerGroups {
		if active != nil {
			if _, ok := active[id]; !ok {
				continue
			}
		}
		if len(selectedSet) == 0 {
			return true
		}
		if _, ok := selectedSet[id]; ok {
			return true
		}
	}
	return false
}

// AccountAutoEligible 判断账号是否满足定时探测：状态为 active，且打开了参与调度。
func AccountAutoEligible(snap *Snapshot) bool {
	if snap == nil {
		return false
	}
	if snap.SchedulingOff {
		return false
	}
	if snap.Status != "" && snap.Status != "active" {
		return false
	}
	return true
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

// CatalogContainsModel 判断该提供商可用模型里有没有本轮探测模型。
func CatalogContainsModel(catalog []string, model string) bool {
	want := catalogModelKey(model)
	if want == "" {
		return false
	}
	for _, item := range catalog {
		got := catalogModelKey(item)
		if got == "" {
			continue
		}
		if got == want {
			return true
		}
		if strings.HasSuffix(got, "*") && strings.HasPrefix(want, strings.TrimSuffix(got, "*")) {
			return true
		}
	}
	return false
}

func catalogModelKey(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.TrimPrefix(normalized, "openai/")
	return fingerprintModelKey(normalized)
}

func isGPT6AstraModel(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	normalized = strings.TrimPrefix(normalized, "openai/")
	return normalized == DefaultProbeModel || strings.HasPrefix(normalized, DefaultProbeModel+"-")
}
