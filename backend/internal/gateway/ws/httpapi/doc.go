// Package httpapi 把 Responses WebSocket 请求接入会话执行器，转换请求帧并连接上游传输。
//
// 阅读入口：
//   - responses_ws.go：处理 Responses WebSocket HTTP 请求。
//   - entry_bindings.go：连接请求授权、模型选择和会话资源。
//   - openai_ws_forwarder_ingress.go：接收客户端连接并启动逐轮转发。
//
// 文件分组：
//   - openai_ws_executor.go、openai_ws_forwarder.go：构造执行器并读取每轮转发参数。
//   - openai_ws_ingress_adapter.go、openai_ws_ingress_execution_adapter.go：适配入站请求和上游连接。
//   - openai_ws_forwarder_payload.go、openai_ws_request_adapter.go：构造请求头和转发载荷。
//   - openai_ws_http_bridge.go：把 WebSocket 轮次转成 HTTP 请求并回传事件。
//   - openai_ws_v2_passthrough_adapter.go、openai_ws_passthrough_execution_adapter.go：接入帧透传。
//   - openai_ws_stream_adapter.go、openai_ws_relay_adapter.go：转换流事件和中继结果。
//   - openai_ws_forwarder_support.go、openai_ws_session_preemption.go：处理失败归因和同会话抢占。
package httpapi
