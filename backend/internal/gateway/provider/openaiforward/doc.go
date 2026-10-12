// Package openaiforward 执行 OpenAI 兼容请求转发和 Anthropic 协议转换。
//
// 阅读入口：
//   - prelude.go：读取请求并选择协议和传输方式。
//   - chat.go：转换 Chat Completions 请求并处理上游响应。
//   - http.go：执行 HTTP 请求和同提供商重试。
//
// 文件分组：
//   - requests.go、transform.go：构造上游请求和转换请求报文。
//   - messages.go、passthrough.go：执行 Messages 请求和透传请求。
//   - raw_chat*.go、raw_fallback*.go：转发 Chat 请求和执行协议回退。
//   - anthropic*.go：构造 Anthropic 请求，读取流并转换响应及用量。
//   - result_projection.go：转换网关、兼容响应和本包的结果类型。
package openaiforward
