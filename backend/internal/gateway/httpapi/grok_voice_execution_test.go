package httpapi

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	xai "github.com/TokenFlux/TokenRouter/internal/upstream/grok"
)

func TestBuildGrokVoiceURL_UsesAPIDefaultForCLIProxyBase(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
			Type: capability.ProviderTypeOAuth,
			Credentials: map[string]any{
				"base_url": xai.DefaultCLIBaseURL,
			},
		},
	}
	url, err := (gatewayprovider.GrokRoutes{Validate: xai.ValidateBaseURL}).Voice(provider, "tts")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/tts", url)

	url, err = (gatewayprovider.GrokRoutes{Validate: xai.ValidateBaseURL}).Voice(provider, "realtime")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/realtime", url)
}

func TestBuildGrokVoiceURL_EmptyBaseFallsBackToAPI(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok,
			Type:        capability.ProviderTypeOAuth,
			Credentials: map[string]any{},
		},
	}
	url, err := (gatewayprovider.GrokRoutes{Validate: xai.ValidateBaseURL}).Voice(provider, "stt")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/stt", url)
}

func TestBuildGrokVoiceURL_RequiresEndpoint(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	_, err := (gatewayprovider.GrokRoutes{Validate: xai.ValidateBaseURL}).Voice(provider, "  ")
	require.Error(t, err)
}

func TestBuildGrokVoiceURL_EncodesCustomVoicePathSegments(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok, Type: capability.ProviderTypeOAuth}}
	got, err := (gatewayprovider.GrokRoutes{Validate: xai.ValidateBaseURL}).Voice(provider, "custom-voices/nlbqfwie/audio")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/custom-voices/nlbqfwie/audio", got)

	_, err = (gatewayprovider.GrokRoutes{Validate: xai.ValidateBaseURL}).Voice(provider, "custom-voices/../audio")
	require.Error(t, err)
}

func TestForwardGrokVoice_RejectsNonGrok(t *testing.T) {
	svc := &GrokExecutor{}
	_, err := svc.ForwardGrokVoice(context.Background(), nil, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformOpenAI}}, "tts", []byte(`{}`), "application/json")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

func TestAwaitGrokRealtimeAudioObservedReadsFlagAfterRelayExits(t *testing.T) {
	errCh := make(chan error, 1)
	var observed atomic.Bool
	go func() {
		observed.Store(true)
		errCh <- io.EOF
	}()
	got, err := xai.AwaitGrokRealtimeAudioObserved(errCh, &observed)
	require.ErrorIs(t, err, io.EOF)
	require.True(t, got, "audioObserved must be read after the relay returns, not before <-errCh")
}

func TestGrokRealtimeEventHasAudio(t *testing.T) {
	require.False(t, xai.GrokRealtimeEventHasAudio([]byte(`{"type":"session.created"}`)))
	require.False(t, xai.GrokRealtimeEventHasAudio([]byte(`{"type":"response.audio_transcript.delta","delta":"hi"}`)))
	require.False(t, xai.GrokRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":""}`)))
	require.True(t, xai.GrokRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":"abc"}`)))
	require.True(t, xai.GrokRealtimeEventHasAudio([]byte(`{"type":"response.output_audio.delta","audio":"abc"}`)))
}

func TestForwardGrokVoice_RejectsUnknownEndpoint(t *testing.T) {
	svc := &GrokExecutor{}
	_, err := svc.ForwardGrokVoice(context.Background(), nil, &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Platform: capability.PlatformGrok}}, "unknown", []byte(`{}`), "application/json")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}
