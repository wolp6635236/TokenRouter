import { getLocale } from '@/i18n'

// formatOrderDateTime 按当前界面语言显示订单时间。
export function formatOrderDateTime(dateStr: string): string {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString(getLocale())
}
