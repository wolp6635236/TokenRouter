package app

import (
	"context"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/provider"
)

// HTTP 故障切换夹具转换存储返回的 token 数据，刷新由凭据组件处理。
type grokCredentialTokenReader struct{ source *grokCredentialHandlerRepo }

func (r grokCredentialTokenReader) GetByID(ctx context.Context, id int64) (*provider.Record, error) {
	v, err := r.source.GetByID(ctx, id)
	return gatewayprovider.ExecutionRecord(v), err
}
