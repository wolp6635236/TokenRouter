// Package bridge 转换 Anthropic、OpenAI 和 Gemini 的请求、响应与流式事件。
//
// 阅读入口：
//   - anthropic_to_responses.go：把 Anthropic 请求转换为 OpenAI Responses 请求。
//   - chatcompletions_responses_bridge.go：转换 Chat Completions 与 Responses 的消息和工具调用。
//   - gemini_native.go：处理 Gemini 请求内容、工具名称和用量。
//
// 文件分组：
//   - anthropic_to_responses*.go、responses_to_anthropic*.go：转换 Anthropic 与 Responses 请求、响应和流状态。
//   - chatcompletions*.go、responses_to_chatcompletions.go：转换 Chat Completions 请求、响应和流状态。
//   - gemini*.go：处理 Gemini 请求选项、内容与流事件。
//   - responses_client_tools.go、responses_namespace.go、responses_tool_adaptation.go：适配客户端工具和命名空间。
//   - responses_input_namespace.go、responses_tool_search_discoveries.go、client_tools_json.go：解析工具输入与发现结果。
//   - compat*.go：处理兼容请求的续接、回放和输出观察。
//   - response_format.go、response_forward_wire.go：整理响应格式和流终态输出。
//   - runtime.go、request_options.go、wire_types.go：定义调用方注入项、转换选项和协议类型。
package bridge
