// Package messageforward 准备 Messages 上游请求，执行转发和协议转换，并处理响应与故障反馈。
//
// 阅读入口：
//   - runtime.go：Messages、计数和 Chat/Responses 转换的调用入口。
//   - attempt.go：单次转发的模型准备、凭据和请求状态。
//   - exchange.go：上游请求构造、重试和响应选项。
//
// 文件分组：
//   - contracts.go、preparation.go：依赖接口、运行参数和 Runtime 构造。
//   - requests.go、mimic.go、metadata.go、cache_policy.go：请求头、客户端特征和缓存策略。
//   - bedrock.go、passthrough.go、conversion.go、count.go、search.go：平台分支、协议转换、计数和搜索。
//   - responses.go、errors.go、transport.go、compatibility.go：响应处理、健康反馈和兼容错误判断。
package messageforward
