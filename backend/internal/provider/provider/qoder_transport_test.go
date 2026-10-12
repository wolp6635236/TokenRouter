package provider

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	egressadapter "github.com/TokenFlux/TokenRouter/internal/egress/provider"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/qoder"
)

type qoderHTTPUpstreamRecorder struct {
	body                string
	userInfoBody        string
	userInfoStatusCode  int
	proxyURL            string
	providerID          int64
	providerConcurrency int
	profileSet          bool
	requests            []*http.Request
}

func TestQoderGatewayRequestDoerUsesHTTPUpstreamProxyAndTLS(t *testing.T) {
	proxyID := int64(11)
	provider := &providercore.Record{
		ID:          66,
		Platform:    capability.PlatformQoder,
		Type:        capability.ProviderTypeCosy,
		Concurrency: 3,
		ProxyID:     &proxyID,
		Proxy: &egress.Proxy{
			ID:       proxyID,
			Protocol: "http",
			Host:     "proxy.example.com",
			Port:     8080,
		},
		Extra: map[string]any{
			"enable_tls_fingerprint": true,
		},
	}
	upstream := &qoderHTTPUpstreamRecorder{
		body: "data: {\"body\":\"[DONE]\"}\n\n",
	}
	doer := QoderRequestDoer(provider, upstream, &egressadapter.TLSProfiles{})
	require.NotNil(t, doer)
	req := httptest.NewRequest(http.MethodPost, "https://api1.qoder.sh/test", strings.NewReader("{}"))

	resp, err := doer(req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, "http://proxy.example.com:8080", upstream.proxyURL)
	require.Equal(t, int64(66), upstream.providerID)
	require.True(t, upstream.profileSet)
}

func (u *qoderHTTPUpstreamRecorder) Do(req *http.Request, proxyURL string, providerID int64, providerConcurrency int) (*http.Response, error) {
	return u.DoWithTLS(req, proxyURL, providerID, providerConcurrency, nil)
}

func (u *qoderHTTPUpstreamRecorder) DoWithTLS(req *http.Request, proxyURL string, providerID int64, providerConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.proxyURL = proxyURL
	u.providerID = providerID
	u.providerConcurrency = providerConcurrency
	u.profileSet = profile != nil
	u.requests = append(u.requests, req)
	body := u.body
	if req.Method == http.MethodGet && strings.Contains(req.URL.Path, qoder.UserInfoPath) {
		body = u.userInfoBody
		if body == "" {
			body = `{"id":"user-1","name":"Qoder User"}`
		}
	}
	statusCode := http.StatusOK
	if req.Method == http.MethodGet && strings.Contains(req.URL.Path, qoder.UserInfoPath) && u.userInfoStatusCode != 0 {
		statusCode = u.userInfoStatusCode
	}
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}
