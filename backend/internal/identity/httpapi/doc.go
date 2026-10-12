// Package httpapi 提供身份认证、用户资料和用户管理的 HTTP 处理器与路由。
//
// 阅读入口：
//   - routes_auth.go：注册登录、会话与第三方认证路由。
//   - session_handler.go：处理注册、密码登录和会话撤销。
//   - admin_user_handler.go：处理管理后台的用户查询与修改。
//
// 文件分组：
//   - routes_auth.go、routes_admin.go、routes_settings.go、routes_user.go、auth_endpoints.go 和 authentication.go：注册路由并分发认证请求。
//   - OAuth 文件（如 email_oauth.go 和 oauth_start.go）、google_one_tap.go 和 pending_handler.go：处理第三方登录、绑定和待完成的注册。
//   - session_handler.go、session_binding.go、logout_cookies.go 和 token_response.go：管理会话、请求设备信息与登录响应。
//   - admin_auth.go、jwt_auth.go、step_up.go 和 backend_mode_guard.go：校验身份、二次验证及后台模式访问权限。
//   - passkey_handler.go 和 totp_handler.go：处理通行密钥及动态口令认证。
//   - user_handler.go、admin_user_handler.go、user_attribute_handler.go、admin_key_settings.go 和 ports.go：处理用户资料、管理操作与处理器所需的接口。
package httpapi
