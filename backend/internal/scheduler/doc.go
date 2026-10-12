// Package scheduler 选择提供商，维护调度快照，并管理请求的并发槽位和会话限制。
//
// 阅读入口：
//   - selection.go：按候选顺序申请槽位、复核提供商并登记尝试租约。
//   - snapshot.go：读取和重建提供商快照，消费调度 outbox 事件。
//   - concurrency.go：申请提供商和用户槽位，跟踪 API Key 并发数与等待队列。
//
// 文件分组：
//   - basic_selection.go、scoring.go、generic_selection*.go、platform_selection*.go、platform_basic_selection.go、gemini_selection.go：筛选候选并按平台和评分策略选择提供商。
//   - selection.go、selection_context.go、lease.go、runtime.go、bucket_lease.go、live_lease.go：传递选择参数，管理请求资源与后台任务的启停。
//   - snapshot*.go、bucket.go、outbox.go：定义快照读写接口、桶标识和事件，发布与重建调度数据。
//   - concurrency.go、wait*.go、image_limiter.go、message_queue*.go：限制并发，处理等待与消息队列。
//   - session*.go、sticky.go：跟踪会话，维护粘性绑定并判断是否需要切换提供商。
//   - rpm*.go、provider_rpm.go、user_rpm_cache.go：检查用户、分组和提供商的每分钟请求限制。
//   - admin_settings*.go、settings*.go、parameters.go、validation_weights.go：读取和校验管理设置，合并运行时参数。
//   - diagnostics.go、diagnostic_service.go：发送日志并生成调度诊断结果。
package scheduler
