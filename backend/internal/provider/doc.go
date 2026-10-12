// Package provider 管理提供商配置、凭据刷新、额度查询和调度健康状态。
//
// 阅读入口：
//   - record.go：提供商记录及其配置读取方法。
//   - admin_contracts.go：管理服务所需的存储、分组和平台操作。
//   - refresh.go：请求期间的凭据刷新、锁协调和条件保存。
//
// 文件分组：
//   - admin*.go、create*.go、management*.go：提供商的创建、编辑、查询和管理操作。
//   - archive*.go、codex_import*.go、crs_*.go：导入导出、凭据匹配和 CRS 同步。
//   - shadow.go、duplicate.go：影子提供商的凭据共享和提供商复制。
//   - credential*.go、token*.go：凭据身份、存储清理、缓存失效和刷新策略。
//   - refresh*.go、background*.go、managed*.go：刷新协调、后台扫描和故障恢复。
//   - openai*.go、claude*.go、antigravity*.go、gemini*.go：各平台的授权、令牌与额度操作。
//   - grok*.go、qoder*.go、vertex*.go：Grok、Qoder 和 Vertex 的凭据与用量操作。
//   - oauth_usage*.go、usage*.go、ollama_usage*.go：用量读取、缓存、窗口统计和数据转换。
//   - health*.go、runtime*.go、retry_cooldown.go：失败观测、运行状态和冷却设置。
//   - model*.go、protocol*.go、endpoint_rule.go：模型映射、协议选择和端点资格。
//   - threshold_settings.go、scheduling_threshold*.go、quota*.go：额度阈值和自动暂停规则。
//   - snapshot.go、clone.go、capacity_snapshot.go：记录副本和调度容量数据。
//   - testing.go、scheduled*.go、probe_runtime.go、import_probes.go：连接测试、定时计划和探测任务。
//   - cn*.go、upstream_queries.go：国产供应商的用量监测和上游查询。
package provider
