// Package requeststate 保存网关请求状态，解析请求字段并管理尝试间共享的执行信息。
//
// 阅读入口：
//   - body.go：解析请求体、消息和会话标识。
//   - execution_hints.go：读写请求上下文中的执行标记。
//   - routing.go：保存分组、协议和模型路由计划。
//
// 文件分组：
//   - body.go、openai_view.go、user_message.go：请求体字段视图和用户消息提取。
//   - routing.go、attempt_route.go、model_body.go：路由计划、候选解析和模型字段替换。
//   - reasoning_effort*.go、thinking.go：推理档位、请求策略和思考状态。
//   - execution_hints.go、health.go、credential_budget.go：执行提示、健康观测和凭据获取预算。
//   - guardian_affinity.go、gemini_signature.go：父会话亲和状态和提供商签名清理。
//   - codex_bootstrap.go、response_tools.go：Codex 启动提示和工具声明读取。
//   - response_failure.go：提供商失败处理结果的一次性读取。
package requeststate
