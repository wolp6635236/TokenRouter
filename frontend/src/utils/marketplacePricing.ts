import type { MarketplaceModelPricing } from '@/types'

// hasImagePricing 判断是否存在有限的非负图片尺寸单价。
function hasImagePricing(pricing: MarketplaceModelPricing): boolean {
  return [
    pricing.image_price_1k,
    pricing.image_price_2k,
    pricing.image_price_4k,
  ].some((value) => typeof value === 'number' && Number.isFinite(value) && value >= 0)
}

// pricingKind 根据定价状态和模式返回展示分类。
export function pricingKind(pricing: MarketplaceModelPricing): 'token' | 'image' | 'unpriced' {
  if (pricing.price_status !== 'priced') {
    return 'unpriced'
  }
  if (pricing.pricing_mode === 'image' && hasImagePricing(pricing)) {
    return 'image'
  }
  if (pricing.pricing_mode === 'token') {
    return 'token'
  }
  return 'unpriced'
}

// formatPriceNumber 按金额大小选择小数位，并使用浏览器的数字格式。
export function formatPriceNumber(value: number): string {
  const abs = Math.abs(value)
  const maximumFractionDigits = abs >= 1 ? 2 : abs >= 0.01 ? 4 : 6
  const minimumFractionDigits = abs >= 1 ? 2 : 4

  return new Intl.NumberFormat(undefined, {
    minimumFractionDigits,
    maximumFractionDigits,
  }).format(value)
}
