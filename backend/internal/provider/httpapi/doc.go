// Package httpapi 处理提供商管理、授权、用量查询和连接测试的 HTTP 请求。
//
// 阅读入口：
//   - routes_admin.go：注册管理接口与各平台授权接口。
//   - management.go：定义管理接口依赖和请求处理器。
//   - test_handler.go：执行连接测试并输出事件流。
//
// 文件分组：
//   - management*.go、archive.go、runtime_presenter.go：管理提供商、导入导出数据并生成状态响应。
//   - openai_oauth.go、claude_oauth.go、gemini_oauth.go、antigravity_oauth.go、grok_oauth.go、qoder_oauth.go：处理各平台授权请求。
//   - codex_import.go、codex_invite_reset.go、crs.go：导入 Codex 凭据并处理邀请和 CRS 操作。
//   - oauth_usage.go、ollama_usage.go、upstream_usage.go：查询授权账户和上游用量。
//   - runtime_settings.go、routes_settings.go、routes_admin.go：读取运行设置并注册路由。
//   - test_handler.go、test_events.go、scheduled_plans.go：执行连接测试、输出事件并管理测试计划。
package httpapi
