package provider_test

import (
	"context"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	acctcore "github.com/TokenFlux/TokenRouter/internal/provider"
)

// 为网关测试提供提供商记录和存储参与能力，刷新逻辑由提供商模块执行。
type tokenSourceFixtureReader struct {
	source gatewayprovider.ExecutionProviderStore
}

func (r tokenSourceFixtureReader) GetByID(ctx context.Context, id int64) (*acctcore.Record, error) {
	v, err := r.source.GetByID(ctx, id)
	return gatewayprovider.ExecutionRecord(v), err
}

func tokenSourceFixtureRepository(repo gatewayprovider.ExecutionProviderStore) acctcore.RefreshRepository {
	if repo == nil {
		return nil
	}
	reader := tokenSourceFixtureReader{repo}
	writer, hasWriter := repo.(acctcore.CredentialRefreshWriter)
	grok, hasGrok := repo.(acctcore.GrokRefreshSuccessWriter)
	if hasWriter && hasGrok {
		return struct {
			acctcore.RefreshRepository
			acctcore.CredentialRefreshWriter
			acctcore.GrokRefreshSuccessWriter
		}{reader, writer, grok}
	}
	if hasWriter {
		return struct {
			acctcore.RefreshRepository
			acctcore.CredentialRefreshWriter
		}{reader, writer}
	}
	if hasGrok {
		return struct {
			acctcore.RefreshRepository
			acctcore.GrokRefreshSuccessWriter
		}{reader, grok}
	}
	return reader
}
