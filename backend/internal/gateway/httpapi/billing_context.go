package httpapi

import (
	billingcore "github.com/TokenFlux/TokenRouter/internal/billing"
)

// 资金来源保存请求已经取得的授权和订阅信息。
const (
	ContextKeyAPIKeyBilling = "api_key_billing"
	ContextKeySubscription  = "subscription"
)

// ContextGetter 让 Gin 上下文读取方法可被轻量测试替代。
type ContextGetter interface {
	Get(key string) (value any, exists bool)
}

// GetAPIKeyBillingContext 读取中间件保存的结算来源。
func GetAPIKeyBillingContext(c ContextGetter) (*billingcore.APIKeyBillingContext, bool) {
	if c == nil {
		return nil, false
	}
	value, exists := c.Get(string(ContextKeyAPIKeyBilling))
	if !exists {
		return nil, false
	}
	billing, ok := value.(*billingcore.APIKeyBillingContext)
	return billing, ok
}
