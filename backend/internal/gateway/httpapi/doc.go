// Package httpapi 注册网关 HTTP 路由，绑定协议入口、上游请求和响应输出。
//
// 阅读入口：
//   - routes.go：注册文本、媒体、模型列表和实时接口及其准入中间件。
//   - openai_text.go：定义文本入口的请求数据和逐步执行接口。
//   - openai_responses_execution.go：执行单次 Responses 请求并分派上游协议。
//
// 文件分组：
//   - authorization.go、api_key*.go 和 routes*.go：认证、Key 模型改写与协议准入。
//   - messages*.go、compatible_text*.go 和 gemini_native*.go：Messages 与 Gemini 的请求处理。
//   - openai_text*.go 和 openai_protocol_execution*.go：OpenAI 文本入口与协议执行。
//   - openai_request*.go、codex*.go 和 session_identity.go：出站报文、客户端身份与会话标识。
//   - openai_response*.go、output*.go 和 response_tool_restoration.go：响应读取、工具名恢复与同步输出。
//   - compact*.go：压缩请求识别、重试与流式心跳。
//   - grok*.go、qoder*.go 和 google*.go：各平台的单次请求、媒体与错误适配。
//   - media*.go、openai_images*.go 和 openai_auxiliary*.go：图片、视频、音频、搜索与计数请求。
//   - live*.go、openai_live*.go 和 ws*.go：实时连接、帧读写与关闭处理。
//   - ops*.go、moderation.go 和 cyber*.go：请求观测、内容审核与风险拦截。
//   - models*.go、search*.go 和 runtime_settings.go：模型目录、搜索响应与网关设置。
//   - wait.go、selected_provider_slot.go 和 completion_submission.go：并发等待、槽位释放与完成记录提交。
package httpapi
