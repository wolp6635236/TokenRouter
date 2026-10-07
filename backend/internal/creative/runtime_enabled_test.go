package creative

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/settings"

	"github.com/stretchr/testify/require"
)

// TestSettingService_IsCreativeEnabled 验证IsCreativeEnabled 是创作台请求期门控读取：显式 "false" 关闭，键缺失或读取失败默认开启。
func TestSettingService_IsCreativeEnabled(t *testing.T) {
	repo := &creativeRuntimeSettingsFixture{values: map[string]string{SettingKeyCreativeEnabled: "false"}}
	svc := NewRuntimeSettings(repo, settings.ErrSettingNotFound)
	require.False(t, svc.IsCreativeEnabled(context.Background()))

	// 键缺失（旧版本库未写入）时默认开启。
	repo = &creativeRuntimeSettingsFixture{values: map[string]string{}}
	svc = NewRuntimeSettings(repo, settings.ErrSettingNotFound)
	require.True(t, svc.IsCreativeEnabled(context.Background()))
}

// creativeRuntimeSettingsFixture 按键读取测试设置，缺键时返回设置不存在错误。
type creativeRuntimeSettingsFixture struct{ values map[string]string }

func (r *creativeRuntimeSettingsFixture) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", settings.ErrSettingNotFound
}
