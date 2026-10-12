// Package grok 适配 xAI 的认证、请求格式、流式响应和媒体接口。
//
// 阅读入口：
//   - request_patch.go：调整 Responses 请求中的模型、工具和历史消息。
//   - responses_execute.go：执行 Responses 请求并汇总用量和完成状态。
//   - oauth.go：定义 OAuth 参数并构造授权与 API 地址。
//
// 文件分组：
//   - oauth*.go、sso_device.go：授权、令牌交换和设备登录。
//   - request_patch.go、model_input.go、compact.go：请求字段、工具调用历史和上下文压缩。
//   - cache*.go、chat*.go、composer_images.go：缓存身份、聊天转换和图片描述。
//   - responses*.go、sse_filter.go：Responses 请求、重试和流式帧过滤。
//   - media*.go、video_content.go、voice*.go、realtime*.go：图片、视频、语音和实时连接。
//   - billing*.go、quota*.go、subscription_tier.go、audio_usage.go：账单、额度、订阅等级和音频用量。
//   - models.go、observed_models.go、standalone_search.go、search_count.go：模型识别、独立搜索和搜索次数统计。
//   - cli_identity.go、endpoint_policy.go、transport_fallback.go：客户端身份、目标地址和传输回退。
//   - errors.go、failure_classifier.go：错误识别、冷却和故障转移条件。
package grok
