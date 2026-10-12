// Package forward 编排单次上游转发，转换请求和响应，并记录失败与用量结果。
//
// 阅读入口：
//   - messages.go：执行 Messages 转发与失败处理。
//   - conversion.go：把客户端请求转换为上游协议并输出目标协议响应。
//   - failure.go：描述失败分类、重试条件和客户端错误。
//
// 文件分组：
//   - messages*.go、gemini*.go 和 count*.go 处理各协议的转发和计数。
//   - anthropic*.go 处理 Anthropic 透传和错误展示。
//   - conversion*.go、output.go 和 mimic.go 处理协议转换、输出与请求伪装。
//   - failure.go、grok_credential_failure.go、invalid_json.go、qoder_error.go 和 upstream_warning.go 处理错误与风控警告。
//   - result.go、openai_result.go、messages_result.go 和 response_observer.go 保存转发结果和响应观察值。
package forward
