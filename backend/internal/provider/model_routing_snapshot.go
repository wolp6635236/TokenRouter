package provider

import (
	"maps"
)

// ModelRoutingSnapshot 保存本次模型匹配需要的规则。
// 在模型匹配时创建，并按需读取动态默认模型。
type ModelRoutingSnapshot struct {
	mapping map[string]string
}

// NewModelRoutingSnapshot 复制模型配置，供本次请求匹配使用。
func NewModelRoutingSnapshot(mapping map[string]string) ModelRoutingSnapshot {
	return ModelRoutingSnapshot{mapping: maps.Clone(mapping)}
}

func (s ModelRoutingSnapshot) Resolve(requested string) (string, bool) {
	return ResolveMappedModel(s.mapping, requested)
}
