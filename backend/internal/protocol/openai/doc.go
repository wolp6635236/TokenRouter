// Package openai 解析和生成 OpenAI Responses、Chat Completions 及 WebSocket 报文。
//
// 阅读入口：
//   - types.go：请求、响应、流事件的数据结构与 JSON 编解码。
//   - response_forward_wire.go：响应模型回填、SSE 帧解析和用量提取。
//   - tool_continuation.go：工具输出续接所需的调用上下文检查。
//
// 文件分组：
//   - types.go、responses_stream_event_wire.go、compatibility_fields.go：协议字段与 JSON 编解码。
//   - compaction_trigger.go、encrypted_replay.go、input_helpers.go、previous_response_id.go：输入项、压缩和历史重放。
//   - tool_continuation.go、tool_schema.go：工具续接与参数结构清理。
//   - response_forward_wire.go、response_lifecycle.go、response_terminal.go：响应读取、生命周期事件和终态处理。
//   - sse_data.go、sse_documents.go、chat_stream_compat.go：SSE 数据行、黏连报文和 Chat 流兼容处理。
//   - ws_events.go、ws_payload.go、ws_warmup.go：WebSocket 事件、请求载荷和预热响应。
//   - forward_usage.go、image_output.go、visible_output.go：用量汇总、图片计数和可见输出识别。
//   - codex_limits.go、quota.go、quota_credits.go：配额窗口及重置信用额度。
//   - oauth_values.go、live.go：认证信息及 Live 会话请求。
//   - lenient_json.go、recorded_effort.go、service_tier_validation.go：JSON 修复、推理档位和服务层级校验。
package openai
