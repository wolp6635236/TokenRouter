// Package batchimage 管理批量图片作业的提交、轮询、结算和资源清理。
//
// 阅读入口：
//   - public.go：校验提交请求，选择提供商并预留任务资金。
//   - worker.go：消费队列任务，维护任务锁并安排重试。
//   - pipeline.go：衔接提供商处理和终态结算。
//
// 文件分组：
//   - job.go、contracts.go：定义作业状态、条目、仓储接口和请求响应类型。
//   - public.go、public_values.go：处理公共 API 请求并生成对外响应。
//   - provider_values.go、registry.go、value_helpers.go：管理提供商注册和作业字段转换。
//   - worker.go、queue.go、runtime.go：消费队列任务并管理后台循环的启停。
//   - processor.go、pipeline.go：轮询上游任务、解析结果并进入结算。
//   - billing.go、pricing.go、settlement.go、recovery.go：预留和结算资金，恢复滞留任务。
//   - download.go、cleanup.go：下载图片和 ZIP 文件，清理过期输入输出。
package batchimage
