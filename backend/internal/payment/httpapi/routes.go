package httpapi

import (
	"github.com/gin-gonic/gin"
)

// PlanEndpoints 是支付页面使用的套餐 HTTP 接口，由 billing 的处理器实现。
type PlanEndpoints interface {
	GetPlans(*gin.Context)
	ListPlans(*gin.Context)
	CreatePlan(*gin.Context)
	UpdatePlan(*gin.Context)
	DeletePlan(*gin.Context)
}

// RouteMiddleware 提供支付路由使用的鉴权、限流和审计处理器。
type RouteMiddleware struct {
	JWT, BackendMode, Panel, Admin, Audit gin.HandlerFunc
}

// WeChatAuthEndpoints 提供支付 OAuth 的发起与回调处理方法。
type WeChatAuthEndpoints interface {
	WeChatPaymentOAuthStart(*gin.Context)
	WeChatPaymentOAuthCallback(*gin.Context)
}

// RegisterRoutes 注册用户支付、公开查询、渠道回调和管理接口。
func RegisterRoutes(
	v1 *gin.RouterGroup,
	paymentHandler *PaymentHandler,
	webhookHandler *PaymentWebhookHandler,
	adminPaymentHandler *AdminHandler,
	planHandler PlanEndpoints,
	guards RouteMiddleware,
) {
	// 用户支付接口使用 JWT 鉴权。
	authenticated := v1.Group("/payment")
	authenticated.Use(guards.JWT)
	authenticated.Use(guards.BackendMode)
	// 面板全局按用户限流
	authenticated.Use(guards.Panel)
	{
		authenticated.GET("/config", paymentHandler.GetPaymentConfig)
		authenticated.GET("/checkout-info", paymentHandler.GetCheckoutInfo)
		authenticated.GET("/plans", planHandler.GetPlans)
		authenticated.GET("/limits", paymentHandler.GetLimits)

		orders := authenticated.Group("/orders")
		{
			orders.POST("", paymentHandler.CreateOrder)
			orders.POST("/verify", paymentHandler.VerifyOrder)
			orders.GET("/my", paymentHandler.GetMyOrders)
			orders.GET("/:id", paymentHandler.GetOrder)
			orders.GET("/:id/invoice", paymentHandler.GetOrderInvoice)
			orders.POST("/:id/cancel", paymentHandler.CancelOrder)
			orders.POST("/:id/refund-request", paymentHandler.RequestRefund)
			orders.GET("/refund-eligible-providers", paymentHandler.GetRefundEligibleProviders)
		}
	}

	// 公开查单优先使用签名恢复令牌。匿名订单号查单返回已保存的状态，供分批升级期间的客户端查询。
	public := v1.Group("/payment/public")
	{
		public.POST("/orders/verify", paymentHandler.VerifyOrderPublic)
		public.POST("/orders/resolve", paymentHandler.ResolveOrderPublicByResumeToken)
	}

	// 渠道回调通过支付提供商的签名鉴权。
	webhook := v1.Group("/payment/webhook")
	{
		// EasyPay 的 GET 回调从查询参数读取通知。
		webhook.GET("/easypay", webhookHandler.EasyPayNotify)
		webhook.POST("/easypay", webhookHandler.EasyPayNotify)
		webhook.POST("/alipay", webhookHandler.AlipayNotify)
		webhook.POST("/wxpay", webhookHandler.WxpayNotify)
		webhook.POST("/stripe", webhookHandler.StripeWebhook)
		webhook.POST("/airwallex", webhookHandler.AirwallexWebhook)
	}

	// 支付管理接口使用管理员鉴权。
	adminGroup := v1.Group("/admin/payment")
	adminGroup.Use(guards.Admin)
	// 支付管理路由独立注册，因此需要单独接入管理员面板限流。
	adminGroup.Use(guards.Panel)
	adminGroup.Use(guards.Audit)
	{
		// Dashboard
		adminGroup.GET("/dashboard", adminPaymentHandler.GetDashboard)

		// Config
		adminGroup.GET("/config", adminPaymentHandler.GetConfig)
		adminGroup.PUT("/config", adminPaymentHandler.UpdateConfig)

		// Orders
		adminOrders := adminGroup.Group("/orders")
		{
			adminOrders.GET("", adminPaymentHandler.ListOrders)
			adminOrders.GET("/:id", adminPaymentHandler.GetOrderDetail)
			adminOrders.GET("/:id/invoice", adminPaymentHandler.GetOrderInvoice)
			adminOrders.POST("/:id/cancel", adminPaymentHandler.CancelOrder)
			adminOrders.POST("/:id/force-expire", adminPaymentHandler.ForceExpireOrder)
			adminOrders.POST("/:id/retry", adminPaymentHandler.RetryFulfillment)
			adminOrders.POST("/:id/refund", adminPaymentHandler.ProcessRefund)
			adminOrders.POST("/:id/refund/query", adminPaymentHandler.QueryAndFinalizeRefund)
		}

		// Subscription Plans
		plans := adminGroup.Group("/plans")
		{
			plans.GET("", planHandler.ListPlans)
			plans.POST("", planHandler.CreatePlan)
			plans.PUT("/:id", planHandler.UpdatePlan)
			plans.DELETE("/:id", planHandler.DeletePlan)
		}

		// Provider Instances
		providers := adminGroup.Group("/providers")
		{
			providers.GET("", adminPaymentHandler.ListProviders)
			providers.POST("", adminPaymentHandler.CreateProvider)
			providers.POST("/test", adminPaymentHandler.TestProvider)
			providers.PUT("/:id", adminPaymentHandler.UpdateProvider)
			providers.DELETE("/:id", adminPaymentHandler.DeleteProvider)
		}
	}
}

// RegisterWeChatAuthRoutes 在身份路由组上注册支付 OAuth 的发起与回调接口。
func RegisterWeChatAuthRoutes(auth *gin.RouterGroup, endpoint WeChatAuthEndpoints) {
	auth.GET("/oauth/wechat/payment/start", endpoint.WeChatPaymentOAuthStart)
	auth.GET("/oauth/wechat/payment/callback", endpoint.WeChatPaymentOAuthCallback)
}
