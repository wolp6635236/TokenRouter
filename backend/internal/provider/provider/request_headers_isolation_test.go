package provider

import (
	"sync"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/provider"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

func headerOverrideProvider() *provider.Record {
	return &provider.Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Credentials: map[string]any{
		"header_override_enabled": true, "header_overrides": map[string]any{"x-test-marker": "configured"},
	}}
}

// TestHeaderOverrideResultIsolation 验证临时修改覆写结果不能影响另一个请求的配置。
func TestHeaderOverrideResultIsolation(t *testing.T) {
	provider := headerOverrideProvider()
	provider.HeaderOverrides()["x-test-marker"] = "changed-by-caller"
	require.Equal(t, "configured", provider.HeaderOverrides()["x-test-marker"])
}

// TestHeaderOverrideConcurrentReads 验证并发解析不可变配置不得回写共享提供商字段。
func TestHeaderOverrideConcurrentReads(t *testing.T) {
	provider := headerOverrideProvider()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			<-start
			for range 20 {
				if provider.HeaderOverrides()["x-test-marker"] != "configured" {
					t.Error("请求头覆写读取结果发生变化")
				}
			}
		})
	}
	close(start)
	wg.Wait()
}
