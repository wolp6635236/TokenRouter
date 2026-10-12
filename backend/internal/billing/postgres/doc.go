// Package postgres 持久化计费余额、套餐、订阅和兑换码，并执行请求与任务的资金结算。
//
// 阅读入口：
//   - settlement.go：扣减请求费用，预占、结算和释放任务资金。
//   - plans.go：读取套餐，事务内维护套餐与分组映射。
//   - user_subscription_repo.go：查询订阅，写入状态、有效期和额度窗口。
//
// 文件分组：
//   - balance.go、available_balance.go：余额更新、初始余额写入和可用余额扣减。
//   - settlement.go、usage_dedup_retention.go：资金结算、任务写入接口和结算去重记录归档。
//   - key_usage.go、member_usage.go：API Key 与团队成员的用量累计和窗口重置。
//   - provider_usage.go、provider_usage_store.go、provider_quota_expressions.go：提供商用量写入、额度周期和提交后通知。
//   - plans.go、user_subscription_repo.go、subscription_transactions.go、user_group_rate_repo.go：套餐、订阅、分组倍率及发放事务。
//   - redeem_code_repo.go、redeem_administration.go、redeem_transactions.go、redeem_writers.go、redeem_update_builder.go：兑换码存储、管理和权益写入。
//   - participants.go、registration_invitation.go：外层事务中的余额、订阅、兑换和注册邀请操作。
//   - entity_helpers.go：事务客户端读取、数据库错误转换和实体转换。
package postgres
