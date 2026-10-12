//go:build integration

package promotion_test

import (
	"os"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/testutil/postgrescontainer"
)

// databaseSuite 在当前测试进程内共享迁移模板。
var databaseSuite postgrescontainer.Suite

// TestMain 在推广测试完成后回收共享容器。
func TestMain(m *testing.M) { os.Exit(databaseSuite.Run(m)) }
