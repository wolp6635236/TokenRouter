package identity_test

// runProfileBackground 异步执行缓存失效，各用例通过同步断言等待完成。
func runProfileBackground(_ string, task func()) bool {
	go task()
	return true
}

func boolPtr(value bool) *bool { return &value }

func float64Ptr(value float64) *float64 { return &value }
