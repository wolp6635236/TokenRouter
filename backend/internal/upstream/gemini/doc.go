// Package gemini 适配 Gemini 的请求认证、协议交换、图片生成和批量任务接口。
//
// 阅读入口：
//   - execute.go：按请求协议执行上游交换并汇总输出观测。
//   - request.go：构造请求计划、认证信息和上游报文。
//   - response.go：解析 Gemini 响应并向输出接口写入事件。
//
// 文件分组：
//   - exchange*.go：Messages、Gemini 和 OpenAI 协议的流式与非流式交换。
//   - response*.go：响应读取、协议输出和完成状态。
//   - request.go、url.go、model_get.go：请求构建、模型地址和模型查询。
//   - images.go、image_observation.go：图片生成、解码和输出计数。
//   - batch*.go：批量客户端调用和 JSONL 编码。
//   - quota_observation.go、retry_rules.go：额度信号和错误重试条件。
package gemini
