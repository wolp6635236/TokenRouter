package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

// 用本地 HTTP 控制隐私查询与管理员修改身份的执行顺序。
type privacyIdentityWriter struct {
	mu      sync.Mutex
	current providercore.Record
}

func TestPrivacyObservationDoesNotOverwriteNewIdentity(t *testing.T) {
	initial := &providercore.Record{ID: 72, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Credentials: map[string]any{"access_token": "original"}, Extra: map[string]any{"privacy_mode": "old"}}
	writer := &privacyIdentityWriter{current: *initial}
	writer.current.Extra = map[string]any{"privacy_mode": "new-identity-mode"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writer.mu.Lock()
		writer.current.Credentials = map[string]any{"access_token": "admin-new"}
		writer.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	svc := providercore.NewPrivacyService(writer, nil, PrivacyOptions(func(string) (*req.Client, error) { return req.C(), nil }, openai.PrivacyEndpoints{Settings: server.URL}))
	svc.ForceOpenAIPrivacy(context.Background(), initial)
	writer.mu.Lock()
	defer writer.mu.Unlock()
	require.Equal(t, "new-identity-mode", writer.current.Extra["privacy_mode"])
}

// UpdatePrivacyModeIfUnchanged 模拟 PostgreSQL 身份比较，发生冲突时跳过写入。
func (w *privacyIdentityWriter) UpdatePrivacyModeIfUnchanged(_ context.Context, v providercore.UsageObservationVersion, mode string) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !providercore.MatchesCredentialVersion(&w.current, v.CredentialVersion) {
		return false, nil
	}
	w.current.Extra["privacy_mode"] = mode
	return true, nil
}
