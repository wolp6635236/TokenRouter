// Package ws 编排 Responses WebSocket 会话、逐轮转发和请求准入。
//
// 阅读入口：
//   - entry.go：接收入站请求，执行准入、提供商选择和故障转移。
//   - ingress.go：解析每轮请求，协调连接租约、HTTP 桥接和响应恢复。
//   - passthrough.go：转发上下行帧，采集每轮用量并调用完成回调。
//
// 文件分组：
//   - entry*.go：入站编排、依赖接口和重试判断。
//   - ingress*.go、openai_session_contract.go：入站会话状态和逐轮回调。
//   - normalize*.go、service_tier_frame.go、policy_error.go：请求归一化、策略检查和策略错误。
//   - passthrough*.go、stream*.go、relay_contract.go：帧透传、单轮响应转发和转发接口。
//   - client*.go、deadline.go：客户端读写、帧过滤和超时控制。
//   - turn*.go、usage*.go：逐轮状态、重试错误和用量快照。
//   - preemption.go：会话抢占、租约续期和失效通知。
//   - parameters.go、runtime.go、settings.go：运行参数校验、配置刷新和设置发布。
package ws
