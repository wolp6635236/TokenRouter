package provider

import (
	"github.com/TokenFlux/TokenRouter/internal/batchimage"
)

// newBatchProviderRegistryForTest 创建包含 Gemini API 和 Vertex 的批量任务注册表。
func newBatchProviderRegistryForTest() *batchimage.Registry[BatchImageProvider] {
	return batchimage.NewRegistry[BatchImageProvider](NewGeminiAPIBatchImageProvider(nil), NewVertexBatchImageProvider(VertexBatchImageProviderOptions{}, nil, nil, nil))
}
