//go:build wireinject

package app

import (
	"github.com/google/wire"

	"github.com/TokenFlux/TokenRouter/internal/payment"
	paymentpostgres "github.com/TokenFlux/TokenRouter/internal/payment/postgres"
)

var (
	// paymentAssemblyProviders 汇总 payment 模块的 Wire provider。
	paymentAssemblyProviders = wire.NewSet(
		providePaymentExpiry,
		providePaymentHTTP,
		providePaymentAdminHTTP,
		providePaymentWebhookHTTP,
		providePaymentConfigCore,
		providePaymentRuntime,
		paymentProviders,
	)

	// 支付 Wire 集合供生成器使用，运行构造函数位于普通 app 文件。
	// ProviderSet 是支付模块的 Wire provider 集合。
	paymentProviders = wire.NewSet(
		paymentpostgres.NewInstanceStore,
		providePaymentEncryptionKey,
		providePaymentRegistry,
		providePaymentLoadBalancer,
		wire.Bind(new(payment.LoadBalancer), new(*payment.DefaultLoadBalancer)),
	)
)
