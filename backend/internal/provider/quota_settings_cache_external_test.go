package provider_test

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/ops"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/settings"
)

// 设置替身提供额度测试和后台缓存刷新所需的值。
type quotaReadFixture struct{ values map[string]string }

func TestGetOpenAIQuotaAutoPauseSettings_ReadsDefaultsFromOpsAdvancedSettings(t *testing.T) {
	repo := newQuotaReadFixture()
	repo.values[ops.SettingKeyOpsAdvancedSettings] = `{"openai_provider_quota_auto_pause":{"default_threshold_5h":0.95,"default_threshold_7d":0.9}}`
	svc := provider.NewQuotaSettingsCache(repo, settings.ErrSettingNotFound, ops.ParseRuntimeQuotaAutoPauseSettings)

	// 同步预热内存缓存后执行断言。
	// GetOpenAIQuotaAutoPauseSettings 在热路径上不阻塞（返回缓存值并异步刷新）；
	// 测试和启动流程使用 Warm 这个同步入口，保证缓存已填充。
	settings := svc.WarmOpenAIQuotaAutoPauseSettings(context.Background())
	if settings.DefaultThreshold5h != 0.95 {
		t.Fatalf("DefaultThreshold5h = %v, want 0.95", settings.DefaultThreshold5h)
	}
	if settings.DefaultThreshold7d != 0.9 {
		t.Fatalf("DefaultThreshold7d = %v, want 0.9", settings.DefaultThreshold7d)
	}

	// 后续 Get 必须命中已预热缓存并返回同一值，不访问 DB；这是热路径不变量。
	cached := svc.GetOpenAIQuotaAutoPauseSettings(context.Background())
	if cached.DefaultThreshold5h != 0.95 || cached.DefaultThreshold7d != 0.9 {
		t.Fatalf("cached read = %+v, want {0.95, 0.9}", cached)
	}
}

// TestGetOpenAIQuotaAutoPauseSettings_ColdCacheNonBlocking 验证热路径不变量：冷缓存 Get 必须立即返回（零默认值），不能阻塞等待 DB。
// 异步刷新器会为后续调用填充缓存。
func TestGetOpenAIQuotaAutoPauseSettings_ColdCacheNonBlocking(t *testing.T) {
	repo := newQuotaReadFixture()
	repo.values[ops.SettingKeyOpsAdvancedSettings] = `{"openai_provider_quota_auto_pause":{"default_threshold_5h":0.7}}`
	svc := provider.NewQuotaSettingsCache(repo, settings.ErrSettingNotFound, ops.ParseRuntimeQuotaAutoPauseSettings)

	start := time.Now()
	settings := svc.GetOpenAIQuotaAutoPauseSettings(context.Background())
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("cold-cache Get must be non-blocking, took %v", elapsed)
	}
	// 冷缓存意味着异步刷新尚未完成，因此返回零默认值。
	if settings.DefaultThreshold5h != 0 || settings.DefaultThreshold7d != 0 {
		t.Fatalf("cold-cache Get = %+v, want zeroes", settings)
	}
}

// TestSetOpenAIQuotaAutoPauseSettings_VisibleImmediately 检查缓存写入后（例如 UpdateOpsAdvancedSettings 调用）下一次读取即可取得新值，
// 且不需要任何 DB roundtrip。
func TestSetOpenAIQuotaAutoPauseSettings_VisibleImmediately(t *testing.T) {
	svc := provider.NewQuotaSettingsCache(newQuotaReadFixture(), settings.ErrSettingNotFound, ops.ParseRuntimeQuotaAutoPauseSettings)

	svc.SetOpenAIQuotaAutoPauseSettings(ops.OpsOpenAIProviderQuotaAutoPauseSettings{
		DefaultThreshold5h: 0.88,
		DefaultThreshold7d: 0.77,
	})

	got := svc.GetOpenAIQuotaAutoPauseSettings(context.Background())
	if got.DefaultThreshold5h != 0.88 || got.DefaultThreshold7d != 0.77 {
		t.Fatalf("after Set, Get = %+v, want {0.88, 0.77}", got)
	}
}

func newQuotaReadFixture() *quotaReadFixture { return &quotaReadFixture{values: map[string]string{}} }

func (r *quotaReadFixture) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", settings.ErrSettingNotFound
}
