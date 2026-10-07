package app

import (
	"context"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/identity"
	identitypostgres "github.com/TokenFlux/TokenRouter/internal/identity/postgres"
	"github.com/TokenFlux/TokenRouter/internal/settings"
	"github.com/stretchr/testify/require"
)

// newUserProfileCore 让资料更新的异步副作用由本测试拥有，退出前等待完成。
func newUserProfileCore(t *testing.T, users identity.UserRepository) *identity.UserService {
	t.Helper()
	tasks := lifecycle.NewTasks()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, tasks.Stop(ctx))
	})
	return identity.NewUserService(users, nil, nil, nil, tasks.Go)
}

// newUserBindingAuth 为资料测试绑定会话和邮箱验证函数，数据库连接设为 nil。
func newUserBindingAuth(users identity.UserRepository, refresh identity.RefreshTokenCache, cfg *config.Config, settings identity.AuthSettings, email identity.AuthEmail) *identity.AuthService {
	options := &identity.AuthOptions{JWT: identity.SessionOptions{Secret: cfg.JWT.Secret, ExpireHour: cfg.JWT.ExpireHour}}
	deps := &identity.AuthDependencies{Users: users, RefreshTokens: refresh, Options: options, Settings: settings, Email: email}
	state := identitypostgres.NewAuthState(nil, deps)
	core := identity.NewAuthService(deps, &identitypostgres.AuthRepository{State: state})
	state.Rules = core
	return core
}

// newUserBindingSettings 组合身份设置读取器，在调用时读取邮箱换绑开关。
func newUserBindingSettings(repo settings.Repository, cfg *config.Config) *identityAuthSettings {
	store := settings.New(repo)
	return provideIdentityAuthSettings(provideIdentitySettings(store), provideGrantSettings(store, cfg, nil), provideSiteDisplay(store, cfg), provideOAuthSettings(store, cfg), providePromotionSettings(store))
}
