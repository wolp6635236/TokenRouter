// Package egress 管理出站代理、TLS 指纹配置和请求传输策略。
//
// 阅读入口：
//   - proxy_admin.go：查询和修改代理，执行连通性探测与质量检查。
//   - request_policy.go：构造每次请求使用的代理、TLS、Header 和目标校验配置。
//   - tls_router_service.go：维护 TLS 路由缓存，按客户端请求匹配指纹配置。
//
// 文件分组：
//   - proxy*.go：代理数据、管理操作、导入导出、过期维护和断流隔离。
//   - tls*.go：TLS 配置、路由规则、缓存、复制和请求配置选择。
//   - collector*.go：TLS 采集记录、短期会话和记录数量限制。
//   - request_policy.go、http2_policy.go、openai_ws_policy.go：请求策略与 HTTP/2、WebSocket 协议选择。
//   - url_policy.go、operator_url.go、usage_url.go、monitor_hosts.go：URL 格式、主机白名单和用量监控地址校验。
//   - ollama*.go、crs_proxy.go：Ollama 云地址与会话 Cookie、CRS 导入代理的匹配与创建。
//   - header_overrides.go、response_headers.go、cloudflare.go：请求头覆盖、响应头筛选和 Cloudflare 响应识别。
//   - diagnostics.go：调用方注入的日志函数。
package egress
