/**
 * mapErrorCategory 按错误阶段和类型返回用户侧分类码。
 * 后端对应实现是 `backend/internal/ops/user_error.go` 中的 `ops.MapUserErrorCategory`。
 * 修改分类时同步两处实现，展示文案使用 i18n `usage.errors.categories.*`。
 */
export function mapErrorCategory(phase?: string | null, errType?: string | null): string {
  switch ((phase || '').toLowerCase()) {
    case 'auth':
      return 'auth'
    case 'routing':
      return 'service_unavailable'
    case 'provider_auth':
    case 'upstream':
    case 'network':
      return 'upstream'
    case 'internal':
      return 'internal'
    case 'request':
      switch ((errType || '').toLowerCase()) {
        case 'rate_limit_error':
          return 'rate_limit'
        case 'billing_error':
        case 'subscription_error':
          return 'quota'
        case 'invalid_request_error':
          return 'invalid_request'
        case 'cyber_policy':
          return 'cyber'
      }
  }
  return 'other'
}
