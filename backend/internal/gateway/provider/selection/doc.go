// Package selection 按分组、模型和运行状态选择网关请求的提供商。
//
// 阅读入口：
//   - runtime.go：构造各平台的选择服务和共享运行参数。
//   - generic.go：执行通用平台的候选过滤、会话绑定和负载选择。
//   - compatible_picker.go：执行 OpenAI 兼容请求的高级调度。
//
// 文件分组：
//   - compatible*.go：OpenAI 兼容请求的候选策略与调度器适配。
//   - generic*.go、gemini*.go：通用平台和 Gemini 的选择流程及调度依赖。
//   - contracts.go、native_dependencies.go、runtime*.go：服务依赖、提供商转换和运行状态。
//   - authorized_group.go、candidate_policy.go、parameters.go：请求分组、候选资格和调度参数。
//   - sticky.go、openai_ws_forwarder_support.go、long_session_model.go：会话绑定与长连接模型选择。
//   - diagnostics.go、diagnostic_ports.go、probes.go：评分诊断和可用性探测。
//   - free_quota.go、grok*.go、proxy_policy.go、gateway_forward.go：额度、冷却、代理和模型限制。
package selection
