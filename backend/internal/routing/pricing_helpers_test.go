package routing

// testPtrFloat64 为测试价卡生成可空金额指针。
func testPtrFloat64(value float64) *float64 { return &value }
func testPtrInt(value int) *int             { return &value }

func testPtrString(value string) *string { return &value }
