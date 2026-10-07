package completion_test

import (
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/TokenFlux/TokenRouter/internal/modelcatalog/provider"
)

// NewBillingService 使用测试目录构造计价器。
func NewBillingService(catalog *provider.Service) *billing.Calculator {
	return testkit.Calculator(catalog, nil)
}
