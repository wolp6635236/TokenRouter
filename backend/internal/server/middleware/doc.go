// Package middleware 提供 HTTP 请求标识、日志、安全响应头和入口限流中间件。
//
// 阅读入口：
//   - client_request_id.go：生成内部请求标识并保存调用方请求标识。
//   - logger.go：记录访问日志和入口拒绝结果。
//   - security_headers.go：生成 CSP nonce 并写入浏览器安全响应头。
//
// 文件分组：
//   - client_request_id.go、request_logger.go、server_timing.go：请求标识、日志上下文和响应耗时。
//   - logger.go、audit_log.go、ingress_reject*.go：访问日志、审计动作和入口拒绝记录。
//   - rate_limiter.go、panel_rate_limit.go：API 与面板请求限流。
//   - cors.go、security_headers.go、request_body_limit.go：跨域策略、安全响应头和请求体大小限制。
//   - locale.go、provider_terminology.go、recovery.go：语言选择、管理字段校验和 panic 恢复。
package middleware
