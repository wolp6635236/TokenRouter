// Package routing 管理分组策略、模型目录和价格配置，并生成请求路由计划。
//
// 阅读入口：
//   - plan.go：根据请求分组和协议生成路由计划，解析候选提供商。
//   - group_admin.go：创建、更新和删除分组，查询可选模型。
//   - requestable.go：按提供商模型、分组映射和白名单解析可请求的模型。
//
// 文件分组：
//   - group*.go、client_group.go：分组配置、管理、容量查询和可用性探测。
//   - plan.go、reasoning.go：请求模型映射、候选协议和推理强度规则。
//   - model*.go、requestable*.go：模型属性、可用性诊断、目录缓存和请求模型解析。
//   - pricing*.go：价格目录、价格配置的读写和缓存、分时计费设置。
//   - marketplace.go、availability_window.go：模型市场展示、报价和可用性时间窗口。
//   - admin*.go、runtime_settings.go：后台模型目录、路由设置的读写和运行时读取。
package routing
