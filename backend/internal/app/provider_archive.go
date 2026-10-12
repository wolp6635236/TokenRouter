package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/config"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	egresspostgres "github.com/TokenFlux/TokenRouter/internal/egress/postgres"
	egressprovider "github.com/TokenFlux/TokenRouter/internal/egress/provider"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	providerhttp "github.com/TokenFlux/TokenRouter/internal/provider/httpapi"
	providerpostgres "github.com/TokenFlux/TokenRouter/internal/provider/postgres"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// grokImportQuotaProbe 将 Grok 额度查询结果转换为导入探测摘要。
type grokImportQuotaProbe struct{ source *provider.GrokQuotaService }

func (p grokImportQuotaProbe) QueryQuota(ctx context.Context, id int64) (*provider.GrokImportProbeResult, error) {
	value, err := p.source.QueryQuota(ctx, id)
	if value == nil {
		return nil, err
	}
	return &provider.GrokImportProbeResult{Model: value.Model, StatusCode: value.StatusCode, HeadersObserved: value.HeadersObserved}, err
}

// provideProviderArchive 为文件导入导出绑定共享的代理、提供商、探测和隐私组件。
func provideProviderArchive(admin *provider.Admin, proxies *egress.ProxyTransfer, privacy *provider.PrivacyService, probes *provider.GrokImportProbeScheduler, grok *provider.GrokQuotaService, tasks *lifecycle.Tasks) *provider.Archive {
	options := provider.ArchiveOptions{
		Now: time.Now, Info: slog.Info, Error: slog.Error, Debug: slog.Debug, DecodeIDToken: provideradapter.DecodeArchiveIDToken, Background: tasks.Go, ForcePrivacy: privacy.ForceAntigravityPrivacy,
		Probe: func(snapshot provider.ProviderSnapshot) {
			probes.Schedule(grokImportQuotaProbe{source: grok}, &snapshot)
		},
	}
	return provider.NewArchive(admin, proxies, options)
}

// provideCodexImporter 绑定提供商管理、备份查询和平台接口。
func provideCodexImporter(admin *provider.Admin, archive *provider.Archive, invalidator provider.TokenCacheInvalidator) *provider.CodexImporter {
	options := provider.CodexImportOptions{OAuthClientID: openai.ClientID, ValidatePrivateKey: func(value string) error { _, err := openai.ParseAgentIdentityPrivateKey(value); return err }}
	if invalidator != nil {
		options.Invalidate = func(ctx context.Context, value *provider.Record) error {
			return invalidator.InvalidateToken(ctx, value)
		}
	}
	options.Now = time.Now
	return provider.NewCodexImporter(admin, archive, options)
}

// provideCodexInvites 绑定提供商用例和平台接口，复用请求侧的 token 和传输实例。
func provideCodexInvites(admin *provider.Admin, proxy egress.ProxyRepository, token *provider.OpenAITokenSource, transport httpclient.UpstreamTransport, profiles *egressprovider.TLSProfiles, routers provideradapter.OpenAITokenRouterReader) *provider.CodexInviteResetService {
	factory := provideradapter.CodexInviteFactory{Token: token, Proxy: proxy.GetByID, Transport: transport, Profiles: profiles, Routers: routers}
	return &provider.CodexInviteResetService{Options: provider.CodexInviteResetOptions{
		Read: admin.GetProvider, Client: factory.Client, NewID: uuid.NewString, Warn: slog.Warn,
	}}
}

// provideCRSSync 为 CRS 同步绑定共享的提供商存储、代理存储和刷新协调器。
func provideCRSSync(providers *providerpostgres.ProviderStore, proxies *egresspostgres.ProxyStore, oauth *provider.ClaudeAuthorization, openai *provider.OpenAIAuthorization, gemini *provider.GeminiAuthorization, cfg *config.Config, coordinator *provider.OAuthRefreshAPI) *provider.CRSSync {
	exchange := &provider.CRSAuthorization{Claude: oauth, OpenAI: openai, Gemini: gemini}
	v := cfg.Security.URLAllowlist
	client := provideradapter.NewCRSClient(provideradapter.CRSClientOptions{Configured: true, AllowlistEnabled: v.Enabled, AllowInsecureHTTP: v.AllowInsecureHTTP, AllowPrivateHosts: v.AllowPrivateHosts, Hosts: v.CRSHosts})
	return provider.NewCRSSync(providers, proxies, client, provider.CRSOptions{Now: time.Now, Warn: slog.Warn, Refresh: exchange.Coordinated(coordinator, provideradapter.GeminiTokenCacheKey)})
}

func provideCRSHTTP(source *provider.CRSSync) *providerhttp.CRSHandler {
	return providerhttp.NewCRSHandler(source)
}
