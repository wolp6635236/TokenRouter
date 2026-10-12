package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// resetViperWithJWTSecret 清空 Viper 与配置路径环境变量，并设置测试用 JWT 密钥。
func resetViperWithJWTSecret(t *testing.T) {
	t.Helper()
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("CONFIG_FILE", "")
	t.Setenv("DATA_DIR", "")
	t.Setenv("JWT_SECRET", strings.Repeat("x", 32))
}

// prepareLegacyConfigTest 清空兼容键的环境变量，并指定当前用例的临时配置文件。
func prepareLegacyConfigTest(t *testing.T, body string) string {
	t.Helper()
	resetViperWithJWTSecret(t)
	for _, item := range legacyConfigKeys {
		for _, key := range []string{item.oldKey, item.newKey} {
			t.Setenv(strings.ToUpper(strings.ReplaceAll(key, ".", "_")), "")
		}
	}
	t.Setenv("GATEWAY_CONNECTION_POOL_ISOLATION", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	t.Setenv("CONFIG_FILE", path)
	return path
}
