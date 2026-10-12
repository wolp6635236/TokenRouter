package modelcatalog

// Reader 提供当前统一目录的候选和元数据，读取失败后的有效快照由实现方保留。
type Reader interface {
	ModelIDs() []string
	ModelEntry(string) Entry
	ModelVersion() string
}
