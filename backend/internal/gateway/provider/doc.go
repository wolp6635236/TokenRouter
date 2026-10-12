// Package provider 将网关执行目标连接到提供商凭据、协议、模型策略和完成处理。
//
// 阅读入口：
//   - execution_provider.go：创建执行目标并读取协议和提供商快照。
//   - route_plan.go：按分组模型映射和客户端协议生成候选路线。
//   - request_credentials.go：取得请求凭据并处理恢复失败。
//
// 文件分组：
//   - execution_*.go、credential_target.go、runtime_readers.go 和 selection.go：执行目标、凭据绑定与选择结果。
//   - model_*.go、compact_models.go 和 compatible_eligibility.go：模型映射、目录展示与请求资格。
//   - completion_*.go、ws_result.go 和 stream_ttft.go：完成快照、用量捕获与响应计时。
//   - grok_*.go、qoder_*.go、ollama_wire_policy.go 和 anthropic_settings.go：平台配置、请求编码与错误策略。
//   - openai_*.go、responses_*.go、codex_request.go 和 compat_continuation.go：OpenAI 请求规范化、续链与失败处理。
//   - health_observation.go、failure_scope.go、invalid_json.go 和 proxy_response_feedback.go：健康观测和失败分类。
//   - image_intent.go、response_image_policy.go 和 creative_*.go：图片意图、图片权限与创作目标。
//   - fast_policy.go、thinking_request.go 和 input_tokens.go：服务档位、思考字段与输入 token 估算。
//   - search_runtime.go、standalone_search*.go 和 stats_pricing.go：搜索执行与价格读取。
//   - upstream_*.go 和 ws_diagnostics.go：上游上下文、请求标识与 WebSocket 诊断。
package provider
