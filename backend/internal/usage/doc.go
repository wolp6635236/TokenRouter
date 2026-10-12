// Package usage 查询使用记录和统计数据，维护用量聚合、清理任务及排行设置。
//
// 阅读入口：
//   - service.go：按用户、API Key、提供商和请求筛选条件查询用量。
//   - dashboard_service.go：管理仪表盘的统计、趋势与排行查询。
//   - dashboard_aggregation_service.go：周期聚合、历史回填和删除后的重算。
//
// 文件分组：
//   - log*.go、request_type.go、views.go：使用记录、查询结果、请求类型和展示数据。
//   - repository.go、query_readers.go、options.go：持久化接口、查询能力和服务配置。
//   - dashboard*.go、query_cache.go：仪表盘查询、统计缓存及聚合作业。
//   - analytics_aggregation.go、aggregation_state.go：聚合仓储接口、水位和回填状态更新。
//   - cleanup*.go：使用记录清理任务的创建、分批执行和取消。
//   - provider*.go：提供商统计结果和 token 滚动窗口。
//   - ranking.go、runtime_settings.go、settings_participant.go、admin_settings_read.go：排行与用户错误展示设置。
package usage
