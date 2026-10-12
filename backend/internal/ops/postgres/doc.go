// Package postgres 读写运维日志、系统指标和告警数据，执行历史数据清理。
//
// 阅读入口：
//   - ops_repo.go：创建 Store，写入和查询错误日志、系统日志。
//   - ops_repo_dashboard.go：查询仪表盘概览并合并预聚合数据。
//   - cleanup.go：按时间分批清理运维数据。
//
// 文件分组：
//   - ops_repo*.go：查询运维数据，维护告警规则、事件和静默记录。
//   - collector_queries.go、ops_sla_sql.go：采集指标并生成 SLA 过滤条件。
//   - ops_ingress_reject_repo.go：批量写入和查询入口拒绝计数。
//   - cleanup.go、historical_ingress_cleanup.go：清理过期日志和历史入口拒绝记录。
//   - advisory.go：用 PostgreSQL 咨询锁协调维护任务。
package postgres
