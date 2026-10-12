// Package ops 采集运维指标和错误，查询监控数据，执行告警、报告与历史清理任务。
//
// 阅读入口：
//   - service.go：错误记录写入、查询及运行设置快照。
//   - metrics_collector.go：主机、连接池和提供商并发指标的周期采样。
//   - dashboard.go：仪表盘概览的数据查询与健康汇总。
//
// 文件分组：
//   - alert*.go、scheduled_report_service.go：告警规则、阈值评估和邮件报告。
//   - cleanup*.go、historical_ingress_cleanup.go：清理计划、保留期限和历史入口拒绝清理。
//   - error*.go、ingress_reject.go、upstream*.go：错误分类、异步入库和入口拒绝计数。
//   - dashboard*.go、health_score.go、histograms.go、trends.go、token_stats*.go：历史监控查询和统计结果。
//   - realtime*.go、concurrency.go、provider_availability.go：实时流量、并发和提供商可用性。
//   - request_details*.go、models.go、user_error.go：请求明细、阶段耗时和用户错误视图。
//   - settings*.go、admin_settings_read.go、composite_settings.go、ignored_status.go：运行设置及其解析。
//   - system_log*.go、log_runtime.go：系统日志写入、查询和运行日志配置。
//   - aggregation_service.go、query_mode.go：监控数据预聚合与查询数据源选择。
//   - options.go、port.go、collector_ports.go、runtime_ports.go、auth_health.go：依赖接口、构造参数和认证健康查询。
//   - release_query.go：版本信息与回退候选查询。
package ops
