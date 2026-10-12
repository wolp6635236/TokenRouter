package egress

// Diagnostics 接收调用方注入的日志函数。
type Diagnostics struct {
	Logf func(component, format string, args ...any)
}

// Log 将组件名和格式化参数传给日志函数。
func (d Diagnostics) Log(component, format string, args ...any) {
	if d.Logf != nil {
		d.Logf(component, format, args...)
	}
}

// diagnosticsOption 返回首个日志配置，未传入时返回零值。
func diagnosticsOption(options []Diagnostics) Diagnostics {
	if len(options) > 0 {
		return options[0]
	}
	return Diagnostics{}
}
