package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
)

type openCodeSessionHTTPUpstream struct {
	request *http.Request
}

func (u *openCodeSessionHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}, nil
}

func (u *openCodeSessionHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, providerID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, providerID, concurrency)
}

func TestOpenCodeSessionForwardedByRawChatCompletionsAfterProviderOverride(t *testing.T) {
	upstream := &openCodeSessionHTTPUpstream{}
	svc := openCodeSessionTestService()
	svc.Requests.Transport = upstream
	if svc.Grok != nil {
		svc.Grok.Transport = svc.Requests.Transport
	}
	provider := openCodeSessionTestProvider("https://opencode.ai/zen/v1")
	c := newOpenCodeSessionTestContext(t, "conversation-789")

	resp, err := svc.Requests.SendChat(
		context.Background(), c, provider,
		"https://opencode.ai/zen/v1/chat/completions", []byte(`{"model":"gpt-5"}`),
		false, "token", "", "",
	)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NotNil(t, upstream.request)
	requireSingleOpenCodeSessionHeader(t, upstream.request.Header, "conversation-789")
}
