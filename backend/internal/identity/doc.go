// Package identity 管理用户身份、注册登录、会话和认证设置。
//
// 阅读入口：
//   - auth_service.go：注册、登录、验证码校验和密码重置。
//   - user_service.go：用户资料、身份绑定和通知邮箱。
//   - session_service.go：访问令牌、刷新令牌和会话撤销。
//
// 文件分组：
//   - admin_*.go：后台用户管理、事务操作和认证设置管理。
//   - auth_*.go、bootstrap.go：认证依赖、邮箱绑定、OAuth 注册和初始管理员创建。
//   - user*.go、risk_status.go：用户数据、资料更新、自定义属性和风险状态。
//   - session_*.go、refresh_token_cache.go、clock.go：会话令牌、缓存接口和时钟读取。
//   - pending*.go、oauth_*.go：待完成认证会话、外部身份和授权规则。
//   - dingtalk_*.go、wechat_oauth.go、linuxdo_oauth.go、oidc_oauth.go、email_oauth.go：各提供商的 OAuth 数据和身份处理。
//   - email_*.go、registration_email_*.go：邮箱验证码和注册邮箱规则。
//   - captcha.go、aliyun_captcha_service.go、tencent_captcha_service.go、turnstile_service.go、totp_service.go、passkey.go：人机验证、双因素验证和通行密钥。
//   - runtime_settings.go、public_settings.go、settings_participant.go、grant_settings*.go：认证开关、公开设置和注册赠送设置。
package identity
