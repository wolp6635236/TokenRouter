package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/gateway/admission"
	"github.com/TokenFlux/TokenRouter/internal/identity/httpapi/authctx"
)

// AuthorizationSubscriptions 保留同一权益实例的读取、校验与窗口维护。
type AuthorizationSubscriptions interface {
	admission.SubscriptionReader
	admission.SubscriptionValidator
}

// APIKeyAuthorizationOptions 提供入口选项、观测接口和请求上下文数据。
type APIKeyAuthorizationOptions struct {
	Authentication keyhttp.AuthenticationOptions
	BindLegacyKey  func(*gin.Context, *apikey.APIKey)
}

// APIKeyIDFromContext 读取已认证主体的 Key ID，缺失或类型不匹配时返回零。
func APIKeyIDFromContext(c *gin.Context) int64 {
	if c == nil {
		return 0
	}
	v, exists := c.Get("api_key")
	if !exists {
		return 0
	}
	apiKey, ok := v.(*apikey.APIKey)
	if !ok || apiKey == nil {
		return 0
	}
	return apiKey.ID
}

func (o APIKeyAuthorizationOptions) loaded(c *gin.Context, k *apikey.APIKey) {
	if o.Authentication.Loaded != nil {
		o.Authentication.Loaded(c, k)
	}
}

func (o APIKeyAuthorizationOptions) business(c *gin.Context) {
	if o.Authentication.BusinessLimited != nil {
		o.Authentication.BusinessLimited(c, "api_key_group_unavailable")
	}
}

func (o APIKeyAuthorizationOptions) reject(c *gin.Context, reason string) {
	if o.Authentication.Rejected != nil {
		o.Authentication.Rejected(c, reason)
	}
}

func (o APIKeyAuthorizationOptions) bind(c *gin.Context, k *apikey.APIKey) {
	c.Set("gateway_effective_key", k)
	if o.BindLegacyKey != nil {
		o.BindLegacyKey(c, k)
	}
}

// EffectiveAPIKey 只供新网关读取已经完成准入的最终 Key 视图。
func EffectiveAPIKey(c *gin.Context) (*apikey.APIKey, bool) {
	value, ok := c.Get("gateway_effective_key")
	if !ok {
		return nil, false
	}
	key, ok := value.(*apikey.APIKey)
	return key, ok
}

func NewAPIKeyAuthorization(apiKeyService *apikey.APIKeyService, subscriptionService AuthorizationSubscriptions, options APIKeyAuthorizationOptions) gin.HandlerFunc {
	options.Authentication.Google = false
	return func(c *gin.Context) {
		access, ok := keyhttp.Authenticate(c, apiKeyService, options.Authentication)
		if !ok {
			return
		}
		apiKey := access.KeyView()
		var err error

		apiKey, err = ResolveCompositeAPIKeyRequest(c, apiKeyService, apiKey)
		if err != nil {
			AbortCompositeKeyError(c, err)
			return
		}
		options.loaded(c, apiKey)
		if abortAuthorizationGroupUnavailable(c, apiKey, options) {
			return
		}
		if abortAuthorizationGroupNotAllowed(c, apiKey, options) {
			return
		}
		// 通用入口在此绑定策略数据，Google 入口使用自己的策略处理。
		requestAccess := *access
		requestAccess.PayerUserID = apiKey.User.ID
		ctx := apikey.WithAccessSnapshot(c.Request.Context(), requestAccess)
		c.Request = c.Request.WithContext(apikey.WithFastModePolicy(ctx, apiKey.FastModePolicy))
		ApplyAPIKeyModelRedirect(c, apiKey)
		// 批任务管理只读取已有数据或释放冻结；即使任务耗尽额度，结果仍应可取回或取消。
		skipBilling := IsAPIKeyUsageRequest(c.Request.Method, c.Request.URL.Path) ||
			IsBatchImageBillingBypassRequest(c.Request.Method, c.Request.URL.Path) ||
			(apiKey.IsComposite && IsGrokVideoTaskRead(c.Request.Method, c.Request.URL.Path))

		// 解析 Key 的结算来源。

		billingContext, billingErr := admission.ResolveFundingFromKey(c.Request.Context(), apiKey, subscriptionService, !skipBilling)
		if billingErr != nil {
			switch {
			case errors.Is(billingErr, billing.ErrPreferredSubscriptionGroup):
				keyhttp.AbortWithError(c, 403, "PREFERRED_SUBSCRIPTION_GROUP_NOT_ALLOWED", billingErr.Error())
			case errors.Is(billingErr, billing.ErrPreferredSubscriptionInvalid):
				keyhttp.AbortWithError(c, 403, "PREFERRED_SUBSCRIPTION_INVALID", billingErr.Error())
			default:
				keyhttp.AbortWithError(c, 500, "INTERNAL_ERROR", "Failed to validate subscription")
			}
			return
		}
		var subscription *billing.UserSubscription
		if billingContext != nil {
			subscription = billingContext.Subscription
		}

		// 消费入口执行资金和 Key 限额检查。

		if !skipBilling {
			checked, failure := admission.CheckConsumption(c.Request.Context(), admission.ConsumptionInput{Status: apiKey.Status, Limits: apiKey, Balance: apiKey.User.Balance, Subscription: subscription}, subscriptionService)
			if failure != nil {
				switch failure.Kind {
				case admission.KeyQuotaExceeded:
					AbortAPIKeyQuotaError(c)
				case admission.KeyExpired:
					keyhttp.AbortWithError(c, 403, "API_KEY_EXPIRED", "The API key has expired.")
				case admission.MaintenanceFailed:
					keyhttp.AbortWithError(c, 500, "SUBSCRIPTION_MAINTENANCE_FAILED", "Failed to maintain subscription usage windows")
				case admission.SubscriptionLimitExceeded:
					keyhttp.AbortWithError(c, 429, "USAGE_LIMIT_EXCEEDED", failure.Error())
				case admission.SubscriptionInvalid:
					keyhttp.AbortWithError(c, 403, "SUBSCRIPTION_INVALID", failure.Error())
				case admission.InsufficientBalance:
					keyhttp.AbortWithError(c, 403, "INSUFFICIENT_BALANCE", "Insufficient account balance")
				}
				return
			}
			subscription = checked
			if billingContext != nil && subscription != nil {
				billingContext.Subscription = subscription
				billingContext.Available = true
			}
		}

		// 写入认证上下文后执行后续处理器。

		if billingContext != nil {
			c.Set("api_key_billing", billingContext)
		}
		if subscription != nil {
			c.Set("subscription", subscription)
		}
		c.Set("api_key", apiKey)
		c.Set("user", authctx.AuthSubject{
			UserID:      apiKey.User.ID,
			Concurrency: apiKey.User.Concurrency,
		})
		c.Set("user_role", apiKey.User.Role)
		options.bind(c, apiKey)
		_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)

		keyhttp.SetAccessPrincipal(c, access)
		release, ok := acquireKeyRequest(c, apiKeyService, apiKey, options)
		if !ok {
			return
		}
		defer release()
		c.Next()
	}
}

// NewGoogleAPIKeyAuthorization 保留 Google 入口的认证与错误时序。
func NewGoogleAPIKeyAuthorization(apiKeyService *apikey.APIKeyService, subscriptionService AuthorizationSubscriptions, options APIKeyAuthorizationOptions) gin.HandlerFunc {
	options.Authentication.Google = true
	return func(c *gin.Context) {
		access, ok := keyhttp.Authenticate(c, apiKeyService, options.Authentication)
		if !ok {
			return
		}
		apiKey := access.KeyView()
		var err error

		apiKey, err = ResolveCompositeAPIKeyRequest(c, apiKeyService, apiKey)
		if err != nil {
			AbortCompositeKeyGoogleError(c, err)
			return
		}
		options.loaded(c, apiKey)
		if code, message, ok := admission.GroupAvailable(apiKey); !ok {
			options.business(c)
			if code == "GROUP_DELETED" {
				options.reject(c, "group_deleted")
			} else {
				options.reject(c, "group_disabled")
			}
			keyhttp.AbortGoogleError(c, 403, message)
			return
		}
		// 专属分组授权校验：用户对该专属分组的授权被撤销后应拒绝（与主中间件一致，防止越权）。
		if !admission.GroupAllowed(apiKey) {
			options.business(c)
			options.reject(c, "group_not_allowed")
			keyhttp.AbortGoogleError(c, 403, "You do not have access to the selected group.")
			return
		}
		ApplyAPIKeyModelRedirect(c, apiKey)
		skipBilling := IsAPIKeyUsageRequest(c.Request.Method, c.Request.URL.Path) ||
			IsBatchImageBillingBypassRequest(c.Request.Method, c.Request.URL.Path) ||
			(apiKey.IsComposite && IsGrokVideoTaskRead(c.Request.Method, c.Request.URL.Path))

		// 非消费请求（包括 /v1/usage 和批任务管理）读取配置快照，资金来源失效时仍可查询和管理。
		billingContext, billingErr := admission.ResolveFundingFromKey(c.Request.Context(), apiKey, subscriptionService, !skipBilling)
		if billingErr != nil {
			switch {
			case errors.Is(billingErr, billing.ErrPreferredSubscriptionGroup):
				keyhttp.AbortGoogleError(c, 403, "Preferred subscription does not allow this group")
			case errors.Is(billingErr, billing.ErrPreferredSubscriptionInvalid):
				keyhttp.AbortGoogleError(c, 403, "Preferred subscription is unavailable")
			default:
				keyhttp.AbortGoogleError(c, 500, "Failed to validate subscription")
			}
			return
		}
		var subscription *billing.UserSubscription
		if billingContext != nil {
			subscription = billingContext.Subscription
		}
		if billingContext != nil {
			c.Set("api_key_billing", billingContext)
		}
		if !skipBilling {
			checked, failure := admission.CheckConsumption(c.Request.Context(), admission.ConsumptionInput{Status: apiKey.Status, Limits: apiKey, Balance: apiKey.User.Balance, Subscription: subscription}, subscriptionService)
			if failure != nil {
				status := 403
				message := ""
				switch failure.Kind {
				case admission.KeyQuotaExceeded:
					status = 429
					message = "The API key quota has been exhausted."
				case admission.KeyExpired:
					message = "The API key has expired."
				case admission.MaintenanceFailed:
					status = 500
					message = "Failed to maintain subscription usage windows"
				case admission.SubscriptionLimitExceeded:
					status = 429
					message = failure.Error()
				case admission.SubscriptionInvalid:
					message = failure.Error()
				case admission.InsufficientBalance:
					message = "Insufficient account balance"
				}
				keyhttp.AbortGoogleError(c, status, message)
				return
			}
			subscription = checked
			if subscription != nil {
				billingContext.Subscription = subscription
			}
		}
		if subscription != nil {
			c.Set("subscription", subscription)
		}

		c.Set("api_key", apiKey)
		c.Set("user", authctx.AuthSubject{
			UserID:      apiKey.User.ID,
			Concurrency: apiKey.User.Concurrency,
		})
		c.Set("user_role", apiKey.User.Role)
		options.bind(c, apiKey)
		_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
		keyhttp.SetAccessPrincipal(c, access)
		release, ok := acquireKeyRequest(c, apiKeyService, apiKey, options)
		if !ok {
			return
		}
		defer release()
		c.Next()
	}
}

func AbortAPIKeyQuotaError(c *gin.Context) {
	const message = "The API key quota has been exhausted."
	if IsOpenAICompatibleAPIKeyRequest(c) {
		AbortOpenAIQuotaError(c, http.StatusTooManyRequests, message)
		return
	}
	keyhttp.AbortWithError(c, http.StatusTooManyRequests, "API_KEY_QUOTA_EXHAUSTED", message)
}

func IsOpenAICompatibleAPIKeyRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}

	path := strings.TrimRight(c.Request.URL.Path, "/")
	for _, root := range []string{
		"/v1/responses",
		"/openai/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// AbortOpenAIQuotaError 输出与 OpenAI 兼容的配额不足响应。
func AbortOpenAIQuotaError(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "insufficient_quota",
			"param":   nil,
			"code":    "insufficient_quota",
		},
	})
	c.Abort()
}

func abortAuthorizationGroupUnavailable(c *gin.Context, key *apikey.APIKey, o APIKeyAuthorizationOptions) bool {
	// 未绑普通 Key 在资金检查之前给出可操作提示；既有任务和本地用量查询仍可访问。
	if key != nil && !key.IsComposite && key.GroupID == nil && !IsAPIKeyUsageRequest(c.Request.Method, c.Request.URL.Path) && !IsBatchImageBillingBypassRequest(c.Request.Method, c.Request.URL.Path) && !IsGrokVideoTaskRead(c.Request.Method, c.Request.URL.Path) {
		o.business(c)
		o.reject(c, "group_unassigned")
		keyhttp.AbortWithError(c, http.StatusForbidden, "GROUP_REQUIRED", "Select a group for this API key in the console")
		return true
	}

	code, message, ok := admission.GroupAvailable(key)
	if ok {
		return false
	}
	o.business(c)
	if code == "GROUP_DELETED" {
		o.reject(c, "group_deleted")
	} else {
		o.reject(c, "group_disabled")
	}
	keyhttp.AbortWithError(c, 403, code, message)
	return true
}

func abortAuthorizationGroupNotAllowed(c *gin.Context, key *apikey.APIKey, o APIKeyAuthorizationOptions) bool {
	if admission.GroupAllowed(key) {
		return false
	}
	o.business(c)
	o.reject(c, "group_not_allowed")
	keyhttp.AbortWithError(c, 403, "GROUP_NOT_ALLOWED", "You do not have access to the selected group.")
	return true
}

// acquireKeyRequest 在协议处理前预占请求额度，非消费接口跳过检查。
func acquireKeyRequest(c *gin.Context, keys *apikey.APIKeyService, key *apikey.APIKey, options APIKeyAuthorizationOptions) (func(), bool) {
	if IsAPIKeyNonConsumingRequest(c.Request.Method, c.Request.URL.Path) || isResponsesTurnAdmission(c) {
		return func() {}, true
	}
	ctx, release, retry, err := keys.AcquireRequest(c.Request.Context(), key)
	if err == nil {
		c.Request = c.Request.WithContext(ctx)
		return release, true
	}
	status := http.StatusTooManyRequests
	code := "API_KEY_RPM_LIMIT_EXCEEDED"
	switch {
	case errors.Is(err, apikey.ErrKeyConcurrencyExceeded):
		code = "API_KEY_CONCURRENCY_LIMIT_EXCEEDED"
	case errors.Is(err, apikey.ErrKeyRPMExceeded):
	default:
		status = http.StatusServiceUnavailable
		code = "API_KEY_LIMITER_UNAVAILABLE"
	}
	if retry > 0 {
		c.Header("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
	}
	if status == http.StatusTooManyRequests && options.Authentication.BusinessLimited != nil {
		options.Authentication.BusinessLimited(c, code)
	}
	if options.Authentication.Google {
		keyhttp.AbortGoogleError(c, status, err.Error())
	} else {
		keyhttp.AbortWithError(c, status, code, err.Error())
	}
	return nil, false
}

// isResponsesTurnAdmission 将 Responses 升级请求的 Key 限制交给逐轮准入。
func isResponsesTurnAdmission(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet || !IsResponsesWSUpgrade(c.Request) {
		return false
	}
	switch strings.TrimRight(c.Request.URL.Path, "/") {
	case "/responses", "/v1/responses", "/backend-api/codex/responses":
		return true
	default:
		return false
	}
}

// GetExecutionAPIKey 从请求上下文读取已认证的 API Key。
func GetExecutionAPIKey(c interface{ Get(string) (any, bool) }) *apikey.APIKey {
	if c == nil {
		return nil
	}
	v, exists := c.Get("api_key")
	if !exists {
		return nil
	}
	apiKey, _ := v.(*apikey.APIKey)
	return apiKey
}
