package settings_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/settings/composite"
	settingskit "github.com/TokenFlux/TokenRouter/internal/settings/testkit"
	"github.com/TokenFlux/TokenRouter/internal/usage"
)

// TestAllowUserViewErrorRequests_PersistsToDB 验证 buildSystemSettingsUpdates 会将
// AllowUserViewErrorRequests 写入 updates map（即最终落库），这是对 bug 的回归测试：
// 该字段曾因漏写而永远无法持久化。
func TestAllowUserViewErrorRequests_PersistsToDB(t *testing.T) {
	// bmUpdateRepoStub 定义在 helpers_test.go。
	// 本测试不触及需要 GetValue 的设置项，getValueFn 设为 nil 即可，无需 stub。
	repo := &bmUpdateRepoStub{}
	svc := settingskit.NewComposite(repo, &config.Config{})

	err := svc.Save(context.Background(), &composite.Snapshot{
		AllowUserViewErrorRequests: true,
	})
	require.NoError(t, err)

	// 断言 updates 中含有该 key，且值为 "true"
	val, ok := repo.updates[usage.SettingKeyAllowUserViewErrorRequests]
	require.True(t, ok, "updates map 中应包含 SettingKeyAllowUserViewErrorRequests，但未找到（bug：buildSystemSettingsUpdates 漏写）")
	require.Equal(t, "true", val)
}
