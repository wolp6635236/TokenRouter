package provider

import (
	"context"
	"log/slog"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

type providerCredentialsUpdater interface {
	UpdateCredentials(context.Context, int64, map[string]any) error
}

type executionCredentialStore struct {
	source   ExecutionProviderStore
	original *ExecutionProvider
}

type executionCredentialFields struct {
	executionCredentialStore
	updater providerCredentialsUpdater
}

// ExecutionTokenSource 定义执行请求读取提供商凭据的接口。
type ExecutionTokenSource interface {
	GetAccessToken(context.Context, *provider.Record) (string, error)
}

func (s executionCredentialStore) Update(ctx context.Context, value *provider.Record) error {
	s.original.Record.Credentials = value.Credentials
	return s.source.Update(ctx, s.original)
}

func (s executionCredentialFields) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	s.original.Record.Credentials = credentials
	return s.updater.UpdateCredentials(ctx, id, credentials)
}

// PersistExecutionCredentials 将执行目标的凭据字段交给 provider 写入。
func PersistExecutionCredentials(ctx context.Context, repo ExecutionProviderStore, value *ExecutionProvider, credentials map[string]any) error {
	if repo == nil || value == nil {
		return nil
	}
	var store provider.CredentialUpdateStore = executionCredentialStore{source: repo, original: value}
	if updater, ok := repo.(providerCredentialsUpdater); ok {
		store = executionCredentialFields{executionCredentialStore: executionCredentialStore{source: repo, original: value}, updater: updater}
	}
	view := &provider.Record{ID: value.Record.ID, Platform: value.Record.Platform, Type: value.Record.Type, ParentProviderID: value.Record.ParentProviderID, QuotaDimension: value.Record.QuotaDimension}
	changed, err := provider.PersistCredentials(ctx, store, view, credentials, slog.Warn)
	if changed {
		value.Record.Credentials = view.Credentials
	}
	return err
}

// ExecutionToken 读取令牌后，将 Gemini/Antigravity 回填的 project 凭据写入执行目标。
func ExecutionToken(ctx context.Context, source ExecutionTokenSource, value *ExecutionProvider) (string, error) {
	record := ExecutionRecord(value)
	token, err := source.GetAccessToken(ctx, record)
	if value != nil && record != nil {
		value.Record.Credentials = record.Credentials
	}
	return token, err
}
