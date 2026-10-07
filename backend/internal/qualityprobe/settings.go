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
	// DefaultScheduleStart 是自动探测窗口起点（本地时区）。
	DefaultScheduleStart = "08:00"
	// DefaultScheduleEnd 是自动探测窗口终点（本地时区，00:00 表示当天午夜）。
	DefaultScheduleEnd = "00:00"
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
	// ScheduleEnabled 为 true 时，自动探测只在 ScheduleStart–ScheduleEnd 内运行。
	ScheduleEnabled bool
	// ScheduleStart / ScheduleEnd 是本地时区的 HH:MM；终点采用左闭右开。
	ScheduleStart string
	ScheduleEnd   string
	// UnscheduleOnDegraded 为 true 时，降智会写入 quality_degraded 临时停调。
	UnscheduleOnDegraded bool
}

// DefaultSettings 返回关闭状态的默认配置。
func DefaultSettings() Settings {
	return Settings{
		Enabled:              false,
		Interval:             30 * time.Minute,
		Model:                DefaultProbeModel,
		Cooldown:             5 * time.Minute,
		MaxAttempts:          3,
		NotifyEmail:          DefaultNotifyEmail,
		GroupIDs:             nil,
		ScheduleEnabled:      true,
		ScheduleStart:        DefaultScheduleStart,
		ScheduleEnd:          DefaultScheduleEnd,
		UnscheduleOnDegraded: true,
	}
}

// ParseClock 把 HH:MM 转成当天从 0 点起的分钟数。
func ParseClock(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if len(value) != 5 || value[2] != ':' {
		return 0, false
	}
	for _, i := range []int{0, 1, 3, 4} {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	hour := int(value[0]-'0')*10 + int(value[1]-'0')
	minute := int(value[3]-'0')*10 + int(value[4]-'0')
	if hour > 23 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

// NormalizeClock 校验并回落非法时钟字符串。
func NormalizeClock(value, fallback string) string {
	if _, ok := ParseClock(value); ok {
		return strings.TrimSpace(value)
	}
	if _, ok := ParseClock(fallback); ok {
		return fallback
	}
	return DefaultScheduleStart
}

// WithinScheduleWindow 判断 now 是否落在 [start, end) 本地时钟窗口。
// start == end 视为全天；start > end 视为跨午夜。
func WithinScheduleWindow(start, end string, now time.Time) bool {
	startMin, startOK := ParseClock(start)
	endMin, endOK := ParseClock(end)
	if !startOK {
		startMin, _ = ParseClock(DefaultScheduleStart)
	}
	if !endOK {
		endMin, _ = ParseClock(DefaultScheduleEnd)
	}
	current := now.Hour()*60 + now.Minute()
	if startMin == endMin {
		return true
	}
	if startMin < endMin {
		return current >= startMin && current < endMin
	}
	return current >= startMin || current < endMin
}

// AutoScheduleActive 判断自动探测在当前时刻是否应运行。
// 总开关 Enabled 由调用方另行检查；本函数只看窗口闸门。
func AutoScheduleActive(cfg Settings, now time.Time) bool {
	if !cfg.ScheduleEnabled {
		return true
	}
	return WithinScheduleWindow(cfg.ScheduleStart, cfg.ScheduleEnd, now.In(time.Local))
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
