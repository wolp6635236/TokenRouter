// Package postgres 存取使用记录，计算用量报表，维护仪表盘和分析预聚合数据。
//
// 阅读入口：
//   - usage_log_repo.go：创建使用记录仓库，定义查询筛选和排行入口。
//   - usage_log_repo_insert.go：写入使用记录，处理批量队列和数据库重试。
//   - dashboard_aggregation_repo.go：维护仪表盘聚合数据和使用记录分区。
//
// 文件分组：
//   - usage_log_repo*.go：使用记录的写入、读取、统计、趋势和仪表盘查询。
//   - dashboard_aggregation_repo.go、usage_analytics_aggregation_repo.go：仪表盘和分析数据的聚合、回填与清理。
//   - provider_report.go、team_reports.go：提供商报表和团队用量报表。
//   - api_key_dashboard_analytics.go、key_totals.go：API Key 仪表盘分析和累计用量。
//   - usage_cleanup_repo.go：使用记录清理任务的存储与执行。
//   - support.go：数据库执行接口、实体转换和事务客户端选择。
package postgres
