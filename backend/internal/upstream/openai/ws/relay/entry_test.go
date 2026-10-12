package openai_ws_v2

import (
	"context"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestRunEntry_DelegatesRelay(t *testing.T) {
	t.Parallel()

	clientConn := newPassthroughTestFrameConn(nil, false)
	upstreamConn := newPassthroughTestFrameConn([]passthroughTestFrame{
		{
			msgType: coderws.MessageText,
			payload: []byte(`{"type":"response.completed","response":{"id":"resp_entry","usage":{"input_tokens":1,"output_tokens":1}}}`),
		},
	}, true)

	result, relayExit := RunEntry(EntryInput{
		Ctx:                context.Background(),
		ClientConn:         clientConn,
		UpstreamConn:       upstreamConn,
		FirstClientMessage: []byte(`{"type":"response.create","model":"gpt-4o","input":[]}`),
	})
	require.Nil(t, relayExit)
	require.Equal(t, "resp_entry", result.RequestID)
}
