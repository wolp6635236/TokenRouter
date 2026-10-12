package openai_ws_v2

import (
	"context"
)

// EntryInput 包含透传转发使用的连接、首帧和选项。
type EntryInput struct {
	Ctx                context.Context
	ClientConn         FrameConn
	UpstreamConn       FrameConn
	FirstClientMessage []byte
	Options            RelayOptions
}

// runCaddyStyleRelay 采用 Caddy reverseproxy 的双向隧道思想：
// 连接建立后并发复制两个方向，任一方向退出后关闭转发。
//
// Reference:
// - Project: caddyserver/caddy (Apache-2.0)
// - Commit: f283062d37c50627d53ca682ebae2ce219b35515
// - Files:
//   - modules/caddyhttp/reverseproxy/streaming.go
//   - modules/caddyhttp/reverseproxy/reverseproxy.go
func runCaddyStyleRelay(
	ctx context.Context,
	clientConn FrameConn,
	upstreamConn FrameConn,
	firstClientMessage []byte,
	options RelayOptions,
) (RelayResult, *RelayExit) {
	return Relay(ctx, clientConn, upstreamConn, firstClientMessage, options)
}

// RunEntry 是 openai_ws_v2 包对外的统一入口。
func RunEntry(input EntryInput) (RelayResult, *RelayExit) {
	return runCaddyStyleRelay(
		input.Ctx,
		input.ClientConn,
		input.UpstreamConn,
		input.FirstClientMessage,
		input.Options,
	)
}
