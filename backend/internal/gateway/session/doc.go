// Package session 管理网关会话标识、摘要匹配、续接状态和会话缓存接口。
//
// 阅读入口：
//   - hash.go：从请求内容和客户端标识派生会话哈希及粘性种子。
//   - digest_session_store.go：保存会话摘要并按最长前缀匹配续接请求。
//   - openai_ws_state_store.go：保存 Responses 会话绑定和响应状态。
//
// 文件分组：
//   - hash.go、qoder_hash.go、openai_content_seed.go、client_id.go：会话哈希、内容种子和客户端标识。
//   - gemini_digest.go、anthropic_prompt_cache.go、digest_session_store.go：消息摘要和提示缓存匹配。
//   - openai_ws_state_store.go、compat_responses.go、reasoning_history.go：响应续接、加密状态和推理历史。
//   - cyber*.go：Cyber 会话键、屏蔽状态、转录和存储适配。
//   - isolation.go、http_response_owner.go：会话隔离和响应归属检查。
//   - live.go、codex_turn_origins.go：实时会话状态和 Codex turn 来源。
//   - stores.go：粘性会话、推理内容和会话控制的缓存接口。
package session
