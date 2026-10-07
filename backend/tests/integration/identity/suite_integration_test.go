//go:build integration

package identity_test

import (
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/testutil/postgrescontainer"
)

// databaseSuite 在当前测试进程内共享迁移模板。
var databaseSuite postgrescontainer.Suite

// runIdentityTests 在身份测试完成后回收共享容器。
func runIdentityTests(m *testing.M) int { return databaseSuite.Run(m) }
