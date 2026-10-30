# OpenCode 上游

本文记录 OpenCode 提供商的模式、模型协议、转发和 Go 用量查询。分组的协议准入与转换规则见[统一协议能力](protocol_capabilities.md)，查询生命周期见[API Key 上游用量查询](upstream_usage.md)。

<a id="opencode_routing"></a>
## 模式与模型协议

提供商使用 `platform=opencode`、`type=apikey`。`credentials.api_key` 保存凭据；`provider_mode` 接受 `payg` 和 `go`，缺省为 `payg`。按量模式使用 Console / Zen，Go 使用订阅入口。

提供商可启用 Messages、Responses、Chat Completions、Gemini GenerateContent 和 SystemOne。实际候选还要检查最终上游模型的原生协议。例如 models.dev 内嵌目录中的 `minimax-m3` 在 payg 使用 Chat，在 Go 使用 Messages。模型名按完整 ID 精确匹配，提供商映射执行后才选协议。

`credentials.opencode_model_protocols` 保存两种模式各自的模型覆盖：

```json
{
  "payg": { "custom-model": "openai_responses" },
  "go": { "custom-model": "anthropic_messages" }
}
```

`credentials.opencode_catalog_protocols` 使用相同的两级结构，保存管理员同步并接受的目录协议。有效协议按人工覆盖、已接受目录、内嵌目录的顺序读取。两种模式分别保存，切换模式不会复用另一模式的模型协议。

模型同步返回上游模型 ID 和当前 models.dev 目录的纯建议，表单用它替换当前模式的目录草稿，人工覆盖由当前表单管理。没有匹配的协议时，响应保留 `model_protocols: {}`，清空当前模式的旧目录建议；缺省该字段表示此次同步不提供协议建议，表单保留原有草稿。删除人工覆盖后恢复目录选择，同步不会把数据库里的旧覆盖重新插回。管理员保存后，已接受目录与人工覆盖一起进入提供商配置；仅同步没有持久化效果。目录未识别的型号需要手动指定协议。

目录保留服务模型的协议及 canonical 属性身份。明确的 `canonical_model_id` 优先；缺失时，只接受完整型号与公共模型 ID 完全一致且作者唯一的匹配，模糊名称保持未知。传给上游的服务模型 ID、模型属性身份和计费身份分别保存。属性解析从当前生效的目录读取 canonical 身份，刷新后的目录优先于内嵌目录；内嵌目录只提供默认协议。

协议来自服务或模型声明的 SDK 和 decision 类型。模型编码规则按已核验的作者身份及协议选择，包含 Claude adaptive thinking、OpenAI 采样条件等；账户认证、配额和站点规则仍由提供商负责。OpenCode 平台身份保持不变，渠道价格继续从默认价卡排除。

提供商的 `upstream_protocols` 继续限制可用协议，空集合关闭新调用。分组准入与已有单步转换规则在选路时生效；SystemOne 保持独立的同步 JSON 入口。候选从同一提供商快照执行一跳映射，保留最终模型和协议，属性身份由目录按最终模型解析；实际请求不能在保留旧协议时重新映射到另一个模型。换号重新解析候选，自动探针也要检查其具体模型的已启用协议。

## 地址与认证

默认服务根地址为 `https://opencode.ai/inference`，Go 为 `https://opencode.ai/inference/go`。`credentials.base_url` 可以覆盖服务根地址；模型同步请求其 `/v1/models`。

| 协议 | 根地址后的默认路径 | `api_base_urls` 地址槽 |
| --- | --- | --- |
| Responses | `/openai/v1/responses` | `responses` |
| Chat | `/openai/v1/chat/completions` | `chat_completions` |
| Messages | `/anthropic/v1/messages` | `anthropic` |
| Gemini | `/google/v1beta/models/{model}:generateContent` | `gemini` |
| SystemOne | `/systemone/v1/systemone` | `systemone` |

地址槽保存完整协议根地址，包括 `/v1` 或 `/v1beta`。`ProtocolTarget.BaseURL` 根据已选协议分派到对应的地址规则，版本段由协议 URL 构造器规范化；Gemini 流式请求使用 `:streamGenerateContent`。覆盖地址同样经过出站 URL 校验，并复用提供商代理和对应传输配置。

各协议使用 `Authorization: Bearer <api_key>`。配置的 Header Override 只负责应用覆盖；随后由明确的 OpenCode 认证步骤移除其他协议的 API Key 头并写入规范化的 Bearer。客户端 User-Agent 优先于提供商配置，缺失时使用配置值或 `TokenRouter`。会话头依次读取 `x-opencode-session`、`session-id`、`session_id`、`conversation_id`、`x-session-affinity`、`x-session-id`，发送为 `X-OpenCode-Session`；缺少稳定会话信息时不生成随机会话 ID。其他 API Key 提供商的请求地址是 `https://opencode.ai` 时（例如指向该地址的 OpenAI 中转），按同样的规则转发会话头，认证保持不变。

OpenCode 的平台身份贯穿使用记录、错误策略和运行监控。错误处理使用实际失败提供商的身份，telemetry 记录这些执行事实。兼容请求复用现有协议编解码、工具调用、流式处理和用量采集。SystemOne 不接受 `stream=true`，Messages token count 使用本地估算。

Gemini 编码的普通失败交给提供商外层重试和换号策略，429 使用共享健康处理及 Retry-After，不采用 Google 每日重置。同一提供商重试的等待取 Retry-After，最多 8 秒；提供商冷却仍按 Retry-After 的完整时间记录。协议专属的签名修复仍有界执行，等待响应取消。上游响应头超时属于提供商失败；只有调用者 Context 已取消或过期时才结束为调用者取消。

原生 HTTP Responses 转发客户端的 `max_output_tokens` 输出上限，并保留 `store` 和 `previous_response_id`。续接请求仍需通过响应归属与来源提供商绑定检查；不可移动的 HTTP 续接不转给另一套上游凭据。上游是否提供所请求的持久性能力由其实际响应决定，此接入没有通过真实账户验证该能力。

原生 Messages 的内部用量保留 uncached input、cache read、cache creation 总量与 5m/1h 明细、output 和 speed。客户端 OpenAI 兼容输出使用 inclusive input，完成记录和计费保留原生桶，防止 TTL 或 speed 在协议转换中丢失。是否可调用某个上游模型仍受其账户权限和服务限制约束。

## 管理与用量

创建和编辑页面在「基本信息」页编辑 `provider_mode` 和 `base_url`，在「请求与协议」页编辑 `api_base_urls` 协议地址槽。模式选择和协议地址槽与 Kimi、Zhipu、DeepSeek 提供商共用控件，留空的地址槽显示由根地址推导的默认地址。模型同步由模型页的通用模型白名单执行；同步结果带有 `model_protocols` 时，表单把它存为当前模式的目录建议。模型协议覆盖表只列出已启用的原生协议。`ModelSyncService.Query` 一次返回 `models` 数组和 `model_protocols` 目录建议。建议来自 app 注入的 `Suggest` 钩子，OpenCode 的实现是 `provider.OpenCodeProtocolSuggestions`：读取在线目录中当前模式的协议表，目录尚未发布时使用内嵌表；已发布的表即使为空也以它为准。创建预览由 HTTP handler 用调用者提供的 Key、根地址和模式构造临时记录，再调用同一个 `Query`，和已保存提供商的同步共用在途跟踪和取消。

已保存提供商的同步只使用保存的 Key、地址、模式和代理，不接受临时配置请求体，空请求或空对象用于兼容调用。MCP 同步权限因此只授权查询已保存目的地。创建预览接受调用者提供的 Key、根地址和模式。编辑已保存的 API Key 提供商时，替换 Key、改变地址或模式后需要先保存，再同步；表单此时不提供已保存提供商的同步入口，防止混用草稿地址和服务器保存的密钥。

OpenCode 测试先解析一份最终模型和协议，再在同一个 `TestRun` 中执行。Messages、Responses、Chat 和 Gemini 使用 `NativeProtocolTest` 的原生交换，Kimi、Zhipu、DeepSeek 的多协议测试也通过这里执行 Messages 和 Responses；它负责协议载荷、流读取，并在上游返回 401 时写入提供商错误状态。最终模型直接写进载荷和 URL，整个探针只做一次模型映射。SystemOne 与 Jev 共用决策交换，401 时不写错误状态。分组探测按探测模型解析出的协议判断 OpenCode 候选能否测试。

Go 使用固定适配器 `opencode_go`，请求 `GET https://opencode.ai/zen/go/v1/usage`，以提供商 API Key 做 Bearer 认证。查询根地址在界面和 API 中独立配置，留空使用官方管理根地址，不复用自定义推理地址。结果包含滚动五小时、周和月窗口的使用百分比、状态及 UTC 重置时间。超过 100% 的已用百分比保留，剩余额度最低为零。

三个窗口均需包含有效的 `percent`、`status` 和 `resetsAt`。异常响应、认证错误、未订阅、限流和超时按查询失败展示。前端通过同一有效配置解析选择 `opencode_go` 并校验结果、缓存和显示资格。单个及批量查询沿用身份复核、独立并发和五分钟浏览器缓存；查询失败不改变转发资格。

payg 不提供上游用量查询，列表仍可展示本地统计。此接入没有钱包余额查询或后台额度监控。用量查询不会根据窗口百分比自动停调，也不会修改 OpenCode 控制台的超额使用设置。

参考：[官方推理 API](https://opencode.ai/v2/docs/console/api/inference/)、[Go](https://opencode.ai/v2/docs/console/go/)、[官方 Go 查询实现](https://github.com/anomalyco/opencode/blob/dev/packages/core/src/usage-limits.ts)。接入协议核验日期：2026-10-26。
