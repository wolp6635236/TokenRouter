// Package provider 将供应商客户端接入提供商管理、测试和健康状态处理。
//
// 阅读入口：
//   - test_targets.go：选择各平台的测试执行器。
//   - upstream_health.go：解析上游错误并调用提供商健康状态规则。
//   - usage_http.go：为用量查询构造请求、代理和 TLS 选项。
//
// 文件分组：
//   - openai*.go、codex*.go、claude*.go 和 anthropic*.go：OpenAI 与 Anthropic 的授权、身份和测试请求。
//   - antigravity*.go、gemini*.go 和 vertex*.go：Google 平台的授权、模型、重试和凭据读取。
//   - grok*.go、qoder*.go 和 cn*.go：Grok、Qoder 与国产供应商的授权、额度和测试执行。
//   - test*.go：平台测试的请求状态、事件和流输出。
//   - model*.go、default_models.go：模型目录、映射和健康状态。
//   - oauth_usage.go、ollama_usage.go、upstream_usage.go 和 usage*.go：用量查询与响应解析。
//   - refresh_errors.go、managed_refresh_key.go 和 privacy.go：刷新错误分类、缓存键和隐私请求。
//   - runtime_status.go、scheduled_plans.go 和 scheduler_score.go：管理页面的运行状态与调度信息。
package provider
