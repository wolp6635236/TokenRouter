package media

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/provider"
)

type voicePortsStub struct {
	selected, released, completed int
	outcomes                      []VoiceOutcome
}

func (p *voicePortsStub) SelectVoice(context.Context, map[int64]struct{}) (provider.ProviderSnapshot, bool, error) {
	p.selected++
	return provider.ProviderSnapshot{ID: int64(p.selected)}, true, nil
}

func (p *voicePortsStub) AcquireVoice(context.Context, provider.ProviderSnapshot) (func(), bool) {
	return func() { p.released++ }, true
}

func (p *voicePortsStub) ForwardVoice(context.Context, provider.ProviderSnapshot, VoiceRequest) VoiceOutcome {
	return p.outcomes[p.selected-1]
}

func (p *voicePortsStub) CompleteVoice(context.Context, provider.ProviderSnapshot, VoiceRequest, *VoiceResult) {
	p.completed++
}

func TestVoiceRetryUsesOriginalFourAttemptBudget(t *testing.T) {
	failure := VoiceOutcome{Err: errors.New("upstream"), RetryNext: true}
	ports := &voicePortsStub{outcomes: []VoiceOutcome{failure, failure, failure, failure}}
	result := RunVoice(context.Background(), VoiceRequest{Endpoint: "tts"}, ports)
	require.NotNil(t, result)
	require.ErrorIs(t, result.Last, failure.Err)
	require.Equal(t, 4, ports.selected)
	require.Equal(t, 4, ports.released)
	require.Zero(t, ports.completed)
	success := &voicePortsStub{outcomes: []VoiceOutcome{failure, {Result: &VoiceResult{RequestID: "audio"}}}}
	require.Nil(t, RunVoice(context.Background(), VoiceRequest{Endpoint: "tts"}, success))
	require.Equal(t, 2, success.released)
	require.Equal(t, 1, success.completed)
}
