package team_test

import (
	"testing"
	"time"
)

// TestMain 将测试进程时区设为 UTC，PostgreSQL 接收固定的时区名称。
func TestMain(m *testing.M) {
	time.Local = time.UTC
	m.Run()
}
