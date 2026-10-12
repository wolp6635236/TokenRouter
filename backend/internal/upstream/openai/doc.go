// Package openai 处理 OpenAI 和 ChatGPT 上游的认证、请求转换、响应读取和用量查询。
//
// 阅读入口：
//   - request_headers.go：构造 Responses HTTP 请求并设置请求头。
//   - response_stream.go：读取 Responses 流并输出客户端事件。
//   - codex_transform.go：转换 Codex 请求中的输入项、工具和指令。
//
// 文件分组：
//   - agent_identity*.go、oauth*.go、pat_client.go：Agent Identity、OAuth 和个人访问令牌认证。
//   - codex_identity.go、provider_identity.go、fingerprint.go、allowed_client.go：客户端身份、提供商会话隔离和请求指纹。
//   - request*.go、responses_lite_tools.go、encrypted_content.go：HTTP 请求、Lite 工具和加密推理内容。
//   - response*.go、first_output_stage.go、silent_refusal.go：响应转换、首输出判断和静默拒绝检测。
//   - ws*.go、live*.go：WebSocket 连接、重放报文和实时认证。
//   - images*.go、image*.go、embeddings.go、alpha_search*.go：图像、嵌入和搜索请求。
//   - privacy_client.go、quota_client.go、codex_usage_headers.go：账户信息、隐私设置和额度。
//   - tool_corrector.go、codex_tool_names.go：Codex 工具名称、参数和响应修正。
//   - test*.go、count_input_tokens.go、http_exchange.go：提供商测试报文、输入 token 计数和 HTTP 交换。
package openai
