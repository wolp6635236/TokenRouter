// Package pricing 解析模型目录与价卡，按用量、计费模式和倍率计算费用。
//
// 阅读入口：
//   - card_resolution.go：合并价卡与基础价格，确定计费模式和价格来源。
//   - calculate.go：计算费用，生成展示价格和上下文区间。
//   - catalog_parse.go：解析目录 JSON，返回价格记录和字段校验结果。
//
// 文件分组：
//   - card.go、card_resolution.go、resolve.go：价卡字段、模型匹配、区间校验和价格覆盖。
//   - calculate.go、types.go：费用计算、展示价格与计算输入输出类型。
//   - catalog_types.go、catalog_parse.go、catalog_rules.go、catalog_extensions.go、catalog_query.go：目录记录、字段解析、规则补充和型号查询。
//   - models_dev.go、model_price.go、default_catalog.go：models.dev 价格转换、计价模型解析和管理员默认价格类型。
//   - media.go、image_unit_price.go、image_billing_size.go、video_billing_resolution.go：图片与视频费用、单价和计费尺寸。
//   - time.go、settings.go：分时价格、时区名校验和计费设置。
//   - provider_stats.go：提供商统计规则匹配和成本计算。
package pricing
