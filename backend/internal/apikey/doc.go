// Package apikey 管理 API Key，执行身份认证和请求额度检查。
//
// 阅读入口：
//   - api_key.go：定义密钥及其状态、限额窗口和副本复制。
//   - service.go：创建、查询和修改密钥，检查团队权限与使用额度。
//   - authenticate.go：按请求上下文认证密钥并解析访问主体。
//
// 文件分组：
//   - auth_cache*.go：缓存认证快照，通过失效通知和 outbox 刷新缓存。
//   - admin.go、managed_key.go、credential_rotation.go：管理员操作、托管密钥和凭据轮换。
//   - composite.go、model_mapping.go、runtime_group.go：复合分组、模型映射和请求分组选择。
//   - access_context.go、request_metadata.go、reauthenticate.go：保存请求身份与元数据，重新验证身份。
//   - request_limits.go、ip.go、invalid_auth_abuse_limiter.go：请求并发、IP 规则和无效认证请求限制。
//   - options.go、clock.go、lifecycle.go：服务配置、时钟和启停。
//   - snapshot_copy.go、published_invalidation.go：复制认证快照数据，发布缓存失效通知。
package apikey
