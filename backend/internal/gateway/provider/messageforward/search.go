package messageforward

import (
	"context"
	"errors"
	"net/http"

	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/gateway/searchtools"
)

// shouldEmulate 调用 Emulator 判断是否模拟搜索，执行结果使用 Messages 用量格式。
func (r *Runtime) shouldEmulate(ctx context.Context, target *gatewayadapter.ExecutionProvider, group *int64, body []byte) bool {
	return r.dependencies.Search.ShouldEmulate(ctx, searchtools.PolicyInput{
		Body:     body,
		Mode:     gatewayadapter.SearchProviderMode(&searchtools.ProviderPolicy{ID: target.Record.ID, Platform: target.Record.Platform, Type: target.Record.Type, Extra: target.Record.Extra}),
		Platform: target.Record.Platform,
		GroupID:  group,
	})
}

func (r *Runtime) emulate(ctx context.Context, output HTTPBoundary, target *gatewayadapter.ExecutionProvider, parsed *requeststate.ParsedRequest) (*forward.Result, error) {
	proxyURL := ""
	if target.Record.ProxyID != nil && target.Record.Proxy != nil {
		proxyURL = target.Record.Proxy.URL()
	}
	result, err := r.dependencies.Search.Execute(ctx, searchtools.Request{
		Body: parsed.Body.Bytes(), Model: parsed.Model, Stream: parsed.Stream,
		ProviderID: target.Record.ID, ProviderName: target.Record.Name,
		ProxyURL: proxyURL, OnAccepted: parsed.OnUpstreamAccepted,
	}, output.SearchOutput())
	if err != nil {
		if _, ok := errors.AsType[*searchtools.ProxyFailure](err); ok {
			return nil, &forward.UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(err.Error())}
		}
		return nil, err
	}
	return &forward.Result{Model: result.Model, Duration: result.Duration, Usage: result.Usage}, nil
}
