// Package schema 定义 TokenRouter 的 Ent 实体字段、关系和索引。
//
// 阅读入口：
//   - user.go：用户身份、余额、访问限制及用户与其他实体的关系。
//   - provider.go：上游凭据、调度状态及提供商分组关系。
//   - group.go：用户可访问的模型分组、请求限制和计费设置。
//
// 文件分组：
//   - user*.go、auth_identity*.go、pending_auth_session.go、identity_adoption_decision.go：用户、登录身份、认证会话和资料采用记录。
//   - team*.go：团队、成员、邀请和所有权转让。
//   - api_key*.go、group.go、provider*.go、proxy.go：访问密钥、分组、上游提供商和代理。
//   - tls_fingerprint*.go、error_passthrough_rule.go：TLS 握手配置和上游错误透传规则。
//   - payment*.go、subscription_plan.go、redeem_code*.go、promo_code*.go：支付、订阅套餐、兑换码和注册优惠码。
//   - creative_run*.go、batch_image*.go：创作任务、输出、待执行动作及批量图片事件。
//   - usage*.go：请求用量和使用记录清理任务。
//   - announcement*.go、setting.go、security_secret.go、idempotency_record.go：公告、系统设置、安全密钥和幂等请求记录。
package schema
