package provider

import (
	"context"
	"net/http"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	egressprovider "github.com/TokenFlux/TokenRouter/internal/egress/provider"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// CodexInviteFactory 在准备邀请操作时解析凭据、代理和专用 TLS 配置。
type CodexInviteFactory struct {
	Token     *provider.OpenAITokenSource
	Proxy     func(context.Context, int64) (*egress.Proxy, error)
	Transport QoderTransport
	Profiles  *egressprovider.TLSProfiles
	Routers   OpenAITokenRouterReader
}

func (f CodexInviteFactory) Client(ctx context.Context, value *provider.Record) (provider.CodexInviteClient, error) {
	token := ""
	if f.Token != nil {
		var err error
		token, err = f.Token.GetAccessToken(ctx, value)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(token) == "" {
		token = value.GetOpenAIAccessToken()
	}
	if strings.TrimSpace(token) == "" {
		return nil, apperror.BadRequest("CODEX_INVITE_RESET_MISSING_TOKEN", "missing OpenAI OAuth access token")
	}
	proxyURL := ""
	if value.ProxyID != nil {
		proxy, err := f.Proxy(ctx, *value.ProxyID)
		if err != nil {
			return nil, err
		}
		if proxy != nil {
			proxyURL = proxy.URL()
		}
	}
	var router *egress.TLSFingerprintRouter
	if f.Routers != nil {
		router = f.Routers.GetRuntimeRouter(value.GetTLSFingerprintRouterID())
		if router != nil && !router.Enabled {
			router = nil
		}
	}
	userAgent := openai.CodexInviteDefaultUserAgent
	if router != nil && strings.TrimSpace(router.CodexInviteResetUserAgent) != "" {
		userAgent = strings.TrimSpace(router.CodexInviteResetUserAgent)
	}
	client := &openai.CodexInviteClient{
		Token: token, UserAgent: userAgent,
		ProviderHeaders: func(headers http.Header) {
			openai.SetChatGPTAccountHeaders(headers, value.GetChatGPTAccountID(), value.IsChatGPTAccountFedRAMP())
		},
	}
	if f.Transport != nil {
		profile := resolveCodexInviteTLS(f.Profiles, value, router)
		client.Do = func(request *http.Request) (*http.Response, error) {
			return f.Transport.DoWithTLS(request, proxyURL, value.ID, value.Concurrency, profile)
		}
	}
	return client, nil
}

// resolveCodexInviteTLS 优先读取邀请重置路由配置，缺省时读取提供商的 TLS 配置。
func resolveCodexInviteTLS(profiles *egressprovider.TLSProfiles, value *provider.Record, router *egress.TLSFingerprintRouter) *tlsfingerprint.Profile {
	if profiles == nil {
		return nil
	}
	if router != nil && router.CodexInviteResetTLSFingerprintProfileID != nil {
		if profile, ok := profiles.ResolveTokenTLSProfileByID(*router.CodexInviteResetTLSFingerprintProfileID); ok {
			return profile
		}
	}
	return profiles.ResolveRequestTLS(egress.TLSSelection{
		Enabled: value.IsTLSFingerprintEnabled(), DirectProfileID: value.GetTLSFingerprintProfileID(),
	})
}
