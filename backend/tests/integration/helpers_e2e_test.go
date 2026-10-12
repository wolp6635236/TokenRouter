//go:build e2e

package integration

import (
	"os"
)

// getEnv 读取环境变量，空值时返回默认值。
func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
