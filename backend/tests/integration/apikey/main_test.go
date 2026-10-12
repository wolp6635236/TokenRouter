package apikey_test

import (
	"testing"
	"time"
)

// TestMain 在运行存储测试前将进程时区设为 UTC。
func TestMain(m *testing.M) {
	time.Local = time.UTC
	m.Run()
}
