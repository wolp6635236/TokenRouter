//go:build !integration

package identity_test

import "testing"

// runIdentityTests 执行无需数据库的测试集合。
func runIdentityTests(m *testing.M) int { return m.Run() }
