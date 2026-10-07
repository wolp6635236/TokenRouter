/**
 * 响应式断点的唯一数值来源,与 Tailwind 默认 screens(sm/md/lg)一一对应。
 * JS 断点判断（matchMedia/useMediaQuery/innerWidth 比较）引用这里的常量。
 * CSS 使用 Tailwind 的 sm:/md:/lg: 变体，测试检查两处取值一致。
 */
export const BREAKPOINT_SM = 640
export const BREAKPOINT_MD = 768
export const BREAKPOINT_LG = 1024

/** max 变体约定:max = min - 1px,与 Tailwind max-* 语义一致,保证 min/max 区间互斥、不存在 1px 重叠带。 */
export const MEDIA_MIN_MD = `(min-width: ${BREAKPOINT_MD}px)`
export const MEDIA_MIN_LG = `(min-width: ${BREAKPOINT_LG}px)`
export const MEDIA_MAX_SM = `(max-width: ${BREAKPOINT_SM - 1}px)`
export const MEDIA_MAX_MD = `(max-width: ${BREAKPOINT_MD - 1}px)`

/** 表格从该宽度开始使用桌面布局；更窄视口统一使用逐行卡片布局。 */
export const TABLE_DESKTOP_MIN_WIDTH = BREAKPOINT_LG

/** 表格外框和内容共用同一媒体查询。 */
export const TABLE_DESKTOP_MEDIA_QUERY = MEDIA_MIN_LG
