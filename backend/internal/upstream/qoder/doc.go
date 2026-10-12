// Package qoder 适配 Qoder 的认证、加密请求、流式响应和会话状态。
//
// 阅读入口：
//   - execute.go：准备并执行上游请求，按调用协议输出响应。
//   - client.go：发送签名请求、解析 SSE 和识别 API 错误。
//   - payload.go：将 Chat、Messages 和 Responses 请求转成 Qoder 报文。
//
// 文件分组：
//   - auth*.go、oauth.go、credential_builder.go、refresh_exchange.go：PAT、设备登录、凭据构建和会话刷新。
//   - session.go、signature.go、encoding.go：加密会话、请求签名和报文编码。
//   - site.go、models.go：站点参数、模型别名与能力。
//   - client.go、execute.go、output_observation.go：请求传输、执行、取消控制和输出观测。
//   - payload.go、stream_conversion.go：请求转换和各协议响应输出。
//   - conversation.go：会话识别、增量历史和提交回滚。
//   - quota_usage.go：额度响应字段与数值解析。
package qoder
