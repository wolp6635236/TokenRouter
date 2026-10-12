package googleforward

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/upstream/antigravity"
)

// TestIsImageGenerationModel_GeminiProImage 测试 gemini-3-pro-image 识别。
func TestIsImageGenerationModel_GeminiProImage(t *testing.T) {
	require.True(t, antigravity.IsImageGenerationModel("gemini-3-pro-image"))
	require.True(t, antigravity.IsImageGenerationModel("gemini-3-pro-image-preview"))
	require.True(t, antigravity.IsImageGenerationModel("models/gemini-3-pro-image"))
}

// TestIsImageGenerationModel_GeminiFlashImage 测试 gemini-2.5-flash-image 识别。
func TestIsImageGenerationModel_GeminiFlashImage(t *testing.T) {
	require.True(t, antigravity.IsImageGenerationModel("gemini-2.5-flash-image"))
	require.True(t, antigravity.IsImageGenerationModel("gemini-2.5-flash-image-preview"))
}

// TestIsImageGenerationModel_RegularModel 测试普通模型不被识别为图片模型。
func TestIsImageGenerationModel_RegularModel(t *testing.T) {
	require.False(t, antigravity.IsImageGenerationModel("claude-3-opus"))
	require.False(t, antigravity.IsImageGenerationModel("claude-sonnet-4-20250514"))
	require.False(t, antigravity.IsImageGenerationModel("gpt-4o"))
	require.False(t, antigravity.IsImageGenerationModel("gemini-2.5-pro")) // 非图片模型
	require.False(t, antigravity.IsImageGenerationModel("gemini-2.5-flash"))
	// 验证不会误匹配包含关键词的自定义模型名
	require.False(t, antigravity.IsImageGenerationModel("my-gemini-3-pro-image-test"))
	require.False(t, antigravity.IsImageGenerationModel("custom-gemini-2.5-flash-image-wrapper"))
}

// TestIsImageGenerationModel_CaseInsensitive 测试大小写不敏感。
func TestIsImageGenerationModel_CaseInsensitive(t *testing.T) {
	require.True(t, antigravity.IsImageGenerationModel("GEMINI-3-PRO-IMAGE"))
	require.True(t, antigravity.IsImageGenerationModel("Gemini-3-Pro-Image"))
	require.True(t, antigravity.IsImageGenerationModel("GEMINI-2.5-FLASH-IMAGE"))
}
