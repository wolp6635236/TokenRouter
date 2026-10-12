package provider

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestPricingConcurrentReadAndReload 验证更新线程整体替换目录时，并发读者只看见一份完整价格，停止后目录仍可读取。
func TestPricingConcurrentReadAndReload(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.json")
	second := filepath.Join(dir, "second.json")
	require.NoError(t, os.WriteFile(first, []byte(`{"providers":{"openai":{"models":{"model":{"cost":{"input":1000000,"output":2000000}}}}}}`), 0o600))
	require.NoError(t, os.WriteFile(second, []byte(`{"providers":{"openai":{"models":{"model":{"cost":{"input":3000000,"output":4000000}}}}}}`), 0o600))
	service := NewService(Options{DataDir: dir}, nil)
	require.NoError(t, service.publishModelsCatalog(readCatalogTestFile(t, first), time.Now(), false))
	var readers sync.WaitGroup
	var inconsistent atomic.Bool
	for range 4 {
		readers.Go(func() {
			for range 200 {
				price := service.GetModelPricing("model")
				if price == nil || price.OutputCostPerToken-price.InputCostPerToken != 1 {
					inconsistent.Store(true)
				}
				_ = service.GetStatus()
				_ = service.Snapshot()
			}
		})
	}
	for i := range 20 {
		file := first
		if i%2 == 0 {
			file = second
		}
		require.NoError(t, service.publishModelsCatalog(readCatalogTestFile(t, file), time.Now(), false))
	}
	readers.Wait()
	require.False(t, inconsistent.Load())
	service.Stop()
	require.NotNil(t, service.GetModelPricing("model"))
}

// TestModelAttributesCanonicalFallbackKeepsPricing 验证服务接入属性回退后仍保留价格来源隔离。
func TestModelAttributesCanonicalFallbackKeepsPricing(t *testing.T) {
	remote := &catalogRemoteFixture{body: []byte(`{
		"models":{"openai/gpt-image-2.5-flare":{
			"modalities":{"input":["text","image"],"output":["image"]}
		}},
		"providers":{
			"azure":{"models":{"gpt-image-2.5-flare":{
				"canonical_model_id":"openai/gpt-image-2.5-flare","cost":{"input":1,"output":2}
			}}},
			"relay":{"models":{"gpt-image-2.5-flare":{
				"canonical_model_id":"openai/gpt-image-2.5-flare","cost":{"input":3,"output":4}
			}}}
		}
	}`)}
	s := NewService(Options{RemoteURL: "https://models.dev/catalog.json", DataDir: t.TempDir()}, remote)
	require.NoError(t, s.ForceUpdate())
	attributes := s.ModelAttributes("gpt-image-2.5-flare")
	require.Equal(t, []string{"text", "image"}, *attributes.InputModalities)
	require.Equal(t, []string{"image"}, *attributes.OutputModalities)
	price := s.GetModelPricing("gpt-image-2.5-flare")
	require.Equal(t, "unpriced", price.Source)
	require.True(t, price.TokenPricingAbsent)
	require.False(t, price.InputPricePresent)
	require.Nil(t, s.ModelAttributes("azure/gpt-image-2.5-flare").InputModalities)
	require.InDelta(t, 1e-6, s.GetModelPricing("azure/gpt-image-2.5-flare").InputCostPerToken, 1e-12)
}

// TestDirectoryReaderTracksPublishedVersion 候选和名称随成功发布更新，坏目录保留上一版。
func TestDirectoryReaderTracksPublishedVersion(t *testing.T) {
	remote := &catalogRemoteFixture{body: []byte(modelsCatalogFixture), etag: "v1"}
	service := NewService(Options{RemoteURL: "https://models.dev/catalog.json", DataDir: t.TempDir()}, remote)
	require.NoError(t, service.ForceUpdate())
	version := service.ModelVersion()
	require.Contains(t, service.ModelIDs(), "claude-test")
	require.Equal(t, "Claude", *service.ModelEntry("claude-test").Attributes.DisplayName)
	require.Nil(t, service.ModelEntry("unknown-model").Attributes.DisplayName)
	remote.mu.Lock()
	remote.body = []byte(`{"providers":{"anthropic":{"models":{"claude-new":{"name":"New name"}}}}}`)
	remote.etag = "v2"
	remote.mu.Unlock()
	require.NoError(t, service.ForceUpdate())
	require.NotEqual(t, version, service.ModelVersion())
	require.Contains(t, service.ModelIDs(), "claude-new")
	require.NotContains(t, service.ModelIDs(), "claude-test")
	version = service.ModelVersion()
	remote.mu.Lock()
	remote.body = []byte(`invalid`)
	remote.mu.Unlock()
	require.Error(t, service.ForceUpdate())
	require.Equal(t, version, service.ModelVersion())
	require.Equal(t, "New name", *service.ModelEntry("claude-new").Attributes.DisplayName)
}

// TestOutputModalityMetadata 按完整型号返回模态，缺失值表示未知。
func TestOutputModalityMetadata(t *testing.T) {
	remote := &catalogRemoteFixture{body: []byte(`{"providers":{"google":{"models":{"arbitrary-output":{"name":"Image","modalities":{"output":["image"]}},"gemini-text-image-name":{"name":"Text","modalities":{"output":["text"]}},"unknown":{"name":"Unknown"}}}}}`)}
	service := NewService(Options{RemoteURL: "https://models.dev/catalog.json", DataDir: t.TempDir()}, remote)
	require.NoError(t, service.ForceUpdate())
	require.Equal(t, []string{"image"}, *service.ModelEntry("arbitrary-output").Attributes.OutputModalities)
	require.Equal(t, []string{"text"}, *service.ModelEntry("gemini-text-image-name").Attributes.OutputModalities)
	require.Nil(t, service.ModelEntry("unknown").Attributes.OutputModalities)
}
