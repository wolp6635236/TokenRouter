package site

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
)

// DisplaySettings 按原时点读取站点名称与前端地址，不增加缓存或批量查询。
type DisplaySettings struct {
	store interface {
		GetValue(context.Context, string) (string, error)
	}
	frontend func() string
}

func NewDisplaySettings(store interface {
	GetValue(context.Context, string) (string, error)
}, frontend func() string,
) *DisplaySettings {
	return &DisplaySettings{store: store, frontend: frontend}
}

func (s *DisplaySettings) GetSiteName(ctx context.Context) string {
	return locale.ReadGroupedText(ctx, s.store, SettingKeySiteTexts, SettingKeySiteName, "TokenRouter")
}

func (s *DisplaySettings) GetFrontendURL(ctx context.Context) string {
	return ReadFrontendURL(ctx, s.store, s.frontend)
}

// GetCustomMenuItemsRaw 保留缺键失败与已保存空串的区别，权限裁决仍由 Pages 执行。
func (s *DisplaySettings) GetCustomMenuItemsRaw(ctx context.Context) string {
	value, err := s.store.GetValue(ctx, SettingKeyCustomMenuItems)
	if err != nil {
		return "[]"
	}
	return value
}
