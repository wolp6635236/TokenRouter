// Package anthropic 构造 Anthropic 上游请求，处理 OAuth 凭据、响应、缓存用量和限流窗口。
//
// 阅读入口：
//   - execute.go：执行请求并向网关输出响应事件。
//   - request.go：构造 Messages 请求及认证头。
//   - stream.go：解析流式响应并汇总用量。
//
// 文件分组：
//   - request*.go、exchange*.go 和 count_tokens_request.go 处理请求构造、传输和透传。
//   - stream*.go、response.go 和 rate_limit_observation.go 处理响应事件、用量和限流。
//   - beta*.go、claude_oauth_body.go、message_cache.go 和 tool_rewrite.go 调整 beta、系统提示词、缓存和工具名。
//   - billing*.go、headers.go、cli_version.go 和 metadata_userid.go 构造客户端指纹。
//   - oauth_client.go、usage_client.go 和 session_digest.go 处理凭据、用量查询和会话摘要。
//   - constants.go、dateline.go、signature_errors.go、thinking_budget.go 和 test_payload.go 提供请求常量、文本修正、错误识别及探测请求。
package anthropic
