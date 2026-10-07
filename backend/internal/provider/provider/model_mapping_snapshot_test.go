package provider

import (
	"sync"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/stretchr/testify/require"
)

// TestModelMappingResultIsolation 验证返回模型规则不能暴露提供商内部派生状态，调用方的临时修改不得污染后续请求。
func TestModelMappingResultIsolation(t *testing.T) {
	provider := &providercore.Record{Platform: capability.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"alias": "target"}}}
	first := providercore.ResolveModelMapping(provider, ModelDefaults())
	first["alias"] = "changed-by-caller"
	require.Equal(t, "target", providercore.ResolveModelMapping(provider, ModelDefaults())["alias"])
}

// TestModelMappingConcurrentReads 验证调度与展示可以并发读取同一配置，纯模型规则的读取不能写入共享提供商字段。
func TestModelMappingConcurrentReads(t *testing.T) {
	provider := &providercore.Record{Platform: capability.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"alias": "target"}}}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			<-start
			for range 20 {
				if providercore.ResolveModelMapping(provider, ModelDefaults())["alias"] != "target" {
					t.Error("模型映射读取结果发生变化")
				}
			}
		})
	}
	close(start)
	wg.Wait()
}
