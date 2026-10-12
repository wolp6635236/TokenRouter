// Package antigravity 通过 Antigravity v1internal 接口转发 Claude 和 Gemini 请求。
//
// 阅读入口：
//   - execute.go：执行单次上游请求并输出客户端协议对应的响应。
//   - request_transformer.go：将 Claude 请求转换为 Gemini 请求并包装 v1internal 字段。
//   - retry_loop.go：处理上游限流、容量不足和重试。
//
// 文件分组：
//   - oauth.go、client.go：OAuth 配置、令牌交换、账户层级和模型查询。
//   - request_transformer.go、payload.go、gemini_types.go：请求格式、模型映射和 JSON Schema 清理。
//   - response*.go、stream_transformer.go：流式与非流式响应读取和协议转换。
//   - retry*.go、quota_errors.go、recovery*.go：重试、额度错误和 Claude/Gemini 请求恢复。
//   - execute.go、probe.go：请求执行与提供商连接测试。
package antigravity
