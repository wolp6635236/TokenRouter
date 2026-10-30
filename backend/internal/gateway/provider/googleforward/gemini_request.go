package googleforward

import (
	"context"
	"errors"
	"net/http"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	gemininative "github.com/TokenFlux/TokenRouter/internal/upstream/gemini"
	"github.com/TokenFlux/TokenRouter/internal/upstream/vertex"
)

// gemini_request.go builds credential-aware Gemini request plans for the
// selected execution target.
// Gemini credential variants share a request plan while obtaining provider
// token and project at build time. API key bases pass through URL validation,
// and Vertex targets supply their project, model location, and action to a
// separate URL callback.

// geminiRequestPlan retains provider inputs in build-time credential and
// endpoint callbacks. Client and upstream stream modes stay separate,
// forceAIStudio selects its request route, and unavailable token services
// return a build error. The read callback supplies inbound client headers for
// provider identity finalization; nil is permitted for callers without an HTTP client.
func (s *Gemini) geminiRequestPlan(value *gatewayprovider.ExecutionProvider, model, action string, native, clientStream, upstreamStream, forceAIStudio bool, read func(string) string) gemininative.RequestPlan {
	plan := gemininative.RequestPlan{
		Mode:           gemininative.CredentialMode(value.Record.Type),
		Model:          model,
		Action:         action,
		Native:         native,
		ClientStream:   clientStream,
		UpstreamStream: upstreamStream,
		ForceAIStudio:  forceAIStudio,
		APIKey:         func() string { return value.View().GetCredential("api_key") },
		BaseURL: func() string {
			return (providercore.ProtocolTarget{Record: value.View(), Protocol: capability.ProtocolGeminiGenerateContent}).BaseURL()
		},
		ValidateURL: s.validateUpstreamBaseURL,
		VertexURL: func(action string, stream bool) (string, error) {
			return vertex.BuildVertexGeminiURL(gatewayprovider.ExecutionProtocolRecord(value).VertexProjectID(vertex.ServiceAccountProjectID), gatewayprovider.ExecutionProtocolRecord(value).VertexLocation(model), model, action, stream)
		},
	}
	plan.Token = func(ctx context.Context) (gemininative.TokenSnapshot, error) {
		if s.Tokens == nil {
			return gemininative.TokenSnapshot{}, errors.New("gemini token provider not configured")
		}
		token, err := gatewayprovider.ExecutionToken(ctx, s.Tokens, value)
		if err != nil {
			return gemininative.TokenSnapshot{}, err
		}
		return gemininative.TokenSnapshot{AccessToken: token, ProjectID: value.View().GetCredential("project_id")}, nil
	}
	plan.ApplyHeaders = func(h http.Header) {
		gatewayprovider.BindExecutionHeaders(value)(h)
		gatewayprovider.FinalizeRequestHeaders(value, "", read, h)
	}
	return plan
}
