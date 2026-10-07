package httpapi

import (
	"net/http"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/gin-gonic/gin"
)

// WriteUnifiedModelsList 使用模型目录中的名称和厂商，未知别名归属于网关。
func (h *ModelsHandler) WriteUnifiedModelsList(c *gin.Context, ids []string) {
	if len(ids) == 0 {
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": []any{}})
		return
	}
	known := make(map[string]gin.H)
	for _, model := range h.catalog.OpenAIModels() {
		known[model.ID] = gin.H{"id": model.ID, "object": "model", "type": "model", "display_name": model.DisplayName, "owned_by": model.OwnedBy, "created": model.Created}
	}
	for _, platform := range []string{capability.PlatformAnthropic, capability.PlatformGemini, capability.PlatformAntigravity, capability.PlatformQoder} {
		for _, model := range h.catalog.ClaudeModels(platform) {
			if _, exists := known[model.ID]; !exists {
				known[model.ID] = gin.H{"id": model.ID, "object": "model", "type": "model", "display_name": model.DisplayName, "created_at": model.CreatedAt}
			}
		}
	}
	for _, model := range h.catalog.GrokModels() {
		known[model.ID] = h.unifiedGrokModel(model)
	}
	models := make([]gin.H, 0, len(ids))
	for _, id := range ids {
		item := known[id]
		if item == nil {
			item = gin.H{"id": id, "object": "model", "type": "model", "display_name": id, "owned_by": "tokenrouter"}
		}
		models = append(models, item)
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": models})
}

// unifiedGrokModel 保留客户端识别推理强度的既有元数据。
func (h *ModelsHandler) unifiedGrokModel(model GrokModel) gin.H {
	item := gin.H{"id": model.ID, "object": "model", "type": "model", "display_name": model.DisplayName, "owned_by": model.OwnedBy, "created": model.Created}
	if GrokModelSupportsConfigurableReasoning(model.ID) {
		item["supportsReasoningEffort"] = true
		item["reasoningEffort"] = "high"
		efforts := []grokReasoningEffortOption{{Value: "low", Label: "Low"}, {Value: "medium", Label: "Medium"}, {Value: "high", Label: "High", Default: true}}
		if h.catalog.GrokSupportsXHigh(model.ID) {
			efforts = append(efforts, grokReasoningEffortOption{Value: "xhigh", Label: "xHigh"})
		}
		item["reasoningEfforts"] = efforts
	}
	return item
}
