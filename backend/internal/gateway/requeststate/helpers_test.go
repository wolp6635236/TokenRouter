package requeststate

// benchmarkIntSink 保存请求解析和字段视图基准的计数结果。
var benchmarkIntSink int

// benchmarkBodySizes 指定请求解析和字段读取基准的目标输入大小。
func benchmarkBodySizes() []struct {
	name  string
	bytes int
} {
	return []struct {
		name  string
		bytes int
	}{
		{name: "4MB", bytes: 4 << 20},
		{name: "8MB", bytes: 8 << 20},
		{name: "16MB", bytes: 16 << 20},
		{name: "32MB", bytes: 32 << 20},
	}
}
