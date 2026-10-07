package app

import (
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/settings"
)

// newOAuthSettingsFixture 构造 OAuth 设置夹具，静态配置由 app 提供，动态设置由身份模块读取。
func newOAuthSettingsFixture(repo settings.Repository, cfg *config.Config) *identity.OAuthSettings {
	return provideOAuthSettings(settings.New(repo), cfg)
}

// readOAuthAdminSettings 为认证设置夹具提供默认并发值，展示规则由身份模块拥有。
func readOAuthAdminSettings(source *identity.OAuthSettings, values map[string]string) *identity.AdminReadSettings {
	return source.ReadAdminSettings(values, func() int { return 0 })
}
