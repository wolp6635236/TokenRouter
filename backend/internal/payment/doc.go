// Package payment 管理支付配置、订单、渠道选择、付款确认、权益发放和退款。
//
// 阅读入口：
//   - runtime.go：汇集下单、订单查询、退款、订单状态处理和渠道绑定服务。
//   - checkout.go：校验下单请求，调用支付渠道并生成下单响应。
//   - fulfillment.go：校验支付通知，确认付款并发放余额或订阅权益。
//
// 文件分组：
//   - configuration*.go、settings_participant.go：读取和保存支付配置，管理渠道实例。
//   - visible_*.go、method_localization.go：管理支付方式的展示来源和多语言名称。
//   - checkout*.go、billing_info.go：处理下单校验、付款人信息和订单快照。
//   - order*.go、query_ports.go、query_types.go、document.go、statistics.go：维护订单状态，查询订单、票据和统计数据。
//   - fulfillment*.go、expiry.go：确认付款、发放权益并定时处理过期订单。
//   - refund_*.go：检查退款条件，调用渠道退款并扣回或恢复权益。
//   - provider_*.go、registry.go、load_balancer.go、instance.go：管理渠道注册、历史订单绑定和实例选择。
//   - amount_rules.go、currency.go、fee.go：计算充值金额、币种精度和手续费。
//   - resume*.go、crypto.go：签发支付恢复令牌，读取加密配置。
//   - types.go、management_ports.go：声明支付请求、渠道接口和管理存储接口。
package payment
