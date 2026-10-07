package provider

import (
	"context"
	"log/slog"
	"time"
)

// newOpenAITokenSourceContract 构造 token 源，缓存、刷新和等待使用生产实现。
func newOpenAITokenSourceContract(cache AccessTokenCache) *OpenAITokenSource {
	return &OpenAITokenSource{
		Cache: cache, Metrics: &OpenAITokenMetricsStore{}, Policy: OpenAIProviderRefreshPolicy(),
		Debug: slog.Debug, Warn: slog.Warn,
	}
}

type openAITokenStateWriter struct {
	RefreshRepository
	setErrorCalls int
	lastErrorMsg  string
}

func (w *openAITokenStateWriter) SetError(_ context.Context, _ int64, message string) error {
	w.setErrorCalls++
	w.lastErrorMsg = message
	return nil
}

type openAITokenBlockRecorder struct {
	providers []*Record
	reasons   []string
}

func (r *openAITokenBlockRecorder) record(value *Record, _ time.Time, reason string) {
	r.providers = append(r.providers, value)
	r.reasons = append(r.reasons, reason)
}
