package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/app/lifecycle"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
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
