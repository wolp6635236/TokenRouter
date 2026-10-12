// Package postgres 读写提供商 PostgreSQL 记录，维护凭据、调度状态和用量观测。
//
// 阅读入口：
//   - provider_store.go：存储构造、配置和事务上下文。
//   - provider_data.go：提供商创建、加载和 Ent 记录转换。
//   - provider_queries.go：筛选、分页和调度候选查询。
//
// 文件分组：
//   - provider_*.go、configuration_write.go：提供商记录、分组关系和配置写入。
//   - credential_refresh.go、grok_credentials.go、refresh_*.go：凭据刷新、错误处置和冷却清理。
//   - usage_*.go、ollama_usage.go、cn_monitor_decision.go、managed_recovery.go：用量观测、平台额度与健康恢复。
//   - events.go、ops_projection.go：变更事件、快照传播和 Ops 负载读取。
//   - group_links.go、proxy_changes.go：分组绑定与代理变更。
//   - scheduled_plans.go：定时测试计划和结果存储。
package postgres
