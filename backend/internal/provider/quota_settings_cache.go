package provider

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// SettingKeyOpsAdvancedSettings 是提供商缓存读取 Ops 设置时使用的键。
	SettingKeyOpsAdvancedSettings = "ops_advanced_settings"

	openAIQuotaAutoPauseSettingsCacheTTL   = 60 * time.Second
	openAIQuotaAutoPauseSettingsErrorTTL   = 5 * time.Second
	openAIQuotaAutoPauseSettingsDBTimeout  = 5 * time.Second
	openAIQuotaAutoPauseSettingsRefreshKey = "openai_quota_auto_pause_settings"
)

// QuotaSettingsCache 保存提供商阈值快照，共享 Ops JSON 通过读取接口解析。
type QuotaSettingsCache struct {
	settingRepo interface {
		GetValue(context.Context, string) (string, error)
	}
	notFound                          error
	decode                            func(string) QuotaAutoPauseSettings
	openAIQuotaAutoPauseSettingsCache atomic.Value
	openAIQuotaAutoPauseSettingsSF    singleflight.Group
}

type cachedOpenAIQuotaAutoPauseSettings struct {
	settings  QuotaAutoPauseSettings
	expiresAt int64
}

// NewQuotaSettingsCache 创建阈值缓存，调用方可同步预热，读取缺失或过期条目时触发异步刷新。
func NewQuotaSettingsCache(repo interface {
	GetValue(context.Context, string) (string, error)
}, notFound error, decode func(string) QuotaAutoPauseSettings,
) *QuotaSettingsCache {
	return &QuotaSettingsCache{settingRepo: repo, notFound: notFound, decode: decode}
}

func (s *QuotaSettingsCache) GetOpenAIQuotaAutoPauseSettings(ctx context.Context) QuotaAutoPauseSettings {
	if s == nil {
		return QuotaAutoPauseSettings{}
	}
	cached, _ := s.openAIQuotaAutoPauseSettingsCache.Load().(*cachedOpenAIQuotaAutoPauseSettings)
	now := time.Now().UnixNano()
	if cached != nil && now < cached.expiresAt {
		return cached.settings
	}
	// 缓存过期或未设置：触发后台刷新，但不阻塞当前请求。
	// singleflight.DoChan 会合并并发刷新；这里有意忽略返回 channel，
	// 结果会通过 atomic 缓存对外可见。
	s.openAIQuotaAutoPauseSettingsSF.DoChan(openAIQuotaAutoPauseSettingsRefreshKey, func() (any, error) {
		s.refreshOpenAIQuotaAutoPauseSettings(context.Background())
		return nil, nil
	})
	if cached != nil {
		return cached.settings // 刷新期间先返回旧值
	}
	return QuotaAutoPauseSettings{}
}

func (s *QuotaSettingsCache) WarmOpenAIQuotaAutoPauseSettings(ctx context.Context) QuotaAutoPauseSettings {
	if s == nil {
		return QuotaAutoPauseSettings{}
	}
	s.refreshOpenAIQuotaAutoPauseSettings(ctx)
	cached, _ := s.openAIQuotaAutoPauseSettingsCache.Load().(*cachedOpenAIQuotaAutoPauseSettings)
	if cached == nil {
		return QuotaAutoPauseSettings{}
	}
	return cached.settings
}

func (s *QuotaSettingsCache) refreshOpenAIQuotaAutoPauseSettings(ctx context.Context) {
	if s == nil || s.settingRepo == nil {
		return
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAIQuotaAutoPauseSettingsDBTimeout)
	defer cancel()

	settings := QuotaAutoPauseSettings{}
	ttl := openAIQuotaAutoPauseSettingsCacheTTL
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyOpsAdvancedSettings)
	if err == nil {
		settings = s.decode(raw)
	} else if !errors.Is(err, s.notFound) {
		// 读取失败时返回缓存值，并缩短下次刷新间隔。
		if prior, _ := s.openAIQuotaAutoPauseSettingsCache.Load().(*cachedOpenAIQuotaAutoPauseSettings); prior != nil {
			settings = prior.settings
		}
		ttl = openAIQuotaAutoPauseSettingsErrorTTL
	}

	s.openAIQuotaAutoPauseSettingsCache.Store(&cachedOpenAIQuotaAutoPauseSettings{
		settings:  settings,
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

func (s *QuotaSettingsCache) SetOpenAIQuotaAutoPauseSettings(settings QuotaAutoPauseSettings) {
	if s == nil {
		return
	}
	settings.DefaultThreshold5h = quotaClamp(settings.DefaultThreshold5h)
	settings.DefaultThreshold7d = quotaClamp(settings.DefaultThreshold7d)
	s.openAIQuotaAutoPauseSettingsCache.Store(&cachedOpenAIQuotaAutoPauseSettings{
		settings:  settings,
		expiresAt: time.Now().Add(openAIQuotaAutoPauseSettingsCacheTTL).UnixNano(),
	})
}

// Apply 发布已提交的阈值，共享 JSON 未变化时使缓存过期并交给后台刷新。
func (s *QuotaSettingsCache) Apply(value QuotaAutoPauseSettings, explicit bool) {
	s.openAIQuotaAutoPauseSettingsSF.Forget(openAIQuotaAutoPauseSettingsRefreshKey)
	if explicit {
		s.SetOpenAIQuotaAutoPauseSettings(value)
	} else if cached, _ := s.openAIQuotaAutoPauseSettingsCache.Load().(*cachedOpenAIQuotaAutoPauseSettings); cached != nil {
		s.openAIQuotaAutoPauseSettingsCache.Store(&cachedOpenAIQuotaAutoPauseSettings{
			settings:  cached.settings,
			expiresAt: 0,
		})
	}
}
