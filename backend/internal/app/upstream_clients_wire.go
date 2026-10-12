//go:build wireinject

package app

import (
	"github.com/google/wire"

	gatewaytransport "github.com/TokenFlux/TokenRouter/internal/gateway/provider/transport"
	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
	"github.com/TokenFlux/TokenRouter/internal/upstream/gemini/codeassist"
	"github.com/TokenFlux/TokenRouter/internal/upstream/grok"
)

// upstreamClientProviders 绑定平台客户端与唯一传输适配器。
var upstreamClientProviders = wire.NewSet(
	anthropic.NewRequestFingerprint,
	provideClaudeUsageFetcher,
	provideOpenAIOAuthClient,
	provideGeminiOAuthClient,
	anthropic.NewOAuthClient,
	wire.Bind(new(provider.ClaudeOAuthClient), new(*anthropic.OAuthClient)),
	grok.NewOAuthClient,
	wire.Bind(new(provider.GrokAuthorizationClient), new(*grok.OAuthClient)),
	codeassist.NewClient,
	wire.Bind(new(provider.GeminiCodeAssistClient), new(*codeassist.Client)),
	codeassist.NewDriveClient,
	provideHTTPUpstream,
	wire.Bind(new(provideradapter.QoderTransport), new(*gatewaytransport.Client)),
)
