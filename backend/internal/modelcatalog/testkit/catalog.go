package testkit

import (
	"slices"

	"github.com/TokenFlux/TokenRouter/internal/modelcatalog"
)

// Catalog 为目录使用方的测试提供可替换版本和明确条目。
type Catalog struct {
	Entries map[string]modelcatalog.Entry
	Version string
}

// New 构造只含测试指定型号的目录，默认展示名称等于 ID。
func New(ids ...string) *Catalog {
	c := &Catalog{Entries: map[string]modelcatalog.Entry{}, Version: "test"}
	for _, id := range ids {
		name := id
		c.Entries[id] = modelcatalog.Entry{Model: id, Attributes: modelcatalog.Attributes{DisplayName: &name}}
	}
	return c
}

// ModelIDs 返回夹具中按 ID 排序的候选。
func (c *Catalog) ModelIDs() []string {
	ids := []string{}
	for id := range c.Entries {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// ModelEntry 返回测试声明的元数据。
func (c *Catalog) ModelEntry(id string) modelcatalog.Entry { return c.Entries[id] }

// ModelVersion 返回测试控制的目录版本。
func (c *Catalog) ModelVersion() string { return c.Version }
