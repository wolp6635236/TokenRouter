// Package billing 计算请求费用，管理余额、订阅和兑换码。
//
// 阅读入口：
//   - calculator.go：按模型价卡和请求用量计算费用。
//   - settlement.go：定义用量结算和任务资金预占、扣款、释放的数据。
//   - subscription_service.go：分配、延长、撤销订阅并维护额度窗口。
//
// 文件分组：
//   - calculator.go、price_resolver.go、default_catalog.go：查询价格配置和模型目录，计算费用。
//   - public_quote.go、media_pricing.go：生成公开报价并查询图片单价。
//   - settlement*.go、funds.go、allocation.go：分配订阅与余额扣款，执行结算并更新缓存。
//   - eligibility*.go、cache.go、key_rate_limits.go、member_quota.go：检查余额、API Key 和团队成员额度。
//   - subscription*.go、plan*.go、clock.go：管理订阅套餐、订阅状态和日期窗口。
//   - redeem_*.go、balance.go、user_summary.go：管理兑换码和调账所需的用户数据。
//   - group_rate_*.go、provider_*.go、window_cost_*.go：管理用户分组倍率和提供商费用窗口。
//   - balance_notifications.go、notification_thresholds.go：判断余额和额度提醒条件。
//   - admin_settings*.go、default_settings.go、settings_participant.go：读取和校验计费设置。
package billing
