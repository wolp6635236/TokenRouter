package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// WriteUnifiedModelsList 使用模型目录中的名称和厂商，未知别名归属于网关。
func (h *ModelsHandler) WriteUnifiedModelsList(c *gin.Context, ids []string) {
	if len(ids) == 0 {
		c.JSON(http.StatusOK, gin.H{"object": "list", "data": []any{}})
		return
	}
	models := make([]gin.H, 0, len(ids))
	for _, id := range ids {
		model := h.catalog.Model(id)
		item := gin.H{"id": id, "object": "model", "type": "model", "display_name": model.DisplayName, "owned_by": model.OwnedBy, "created": model.Created}
		if GrokModelSupportsConfigurableReasoning(id) {
			item = h.unifiedGrokModel(GrokModel{ID: id, Object: "model", Type: "model", OwnedBy: "xai", DisplayName: model.DisplayName})
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
