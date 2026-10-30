package provider

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// model_sync_test.go checks input isolation, catalogue ownership, and shutdown
// behavior. A live model adapter may mutate its input or delay returning after shutdown
// cancellation. A blocked fetch makes those effects observable, checking that
// the caller's credentials stay isolated and that stop rejects another query
// while retaining its first timeout result.

// TestModelSyncOwnsInflightQueryAndInput blocks a fetch after mutating its
// copied credential map, then times out shutdown. The original token stays
// intact, later queries fail as stopped, and the released query sees
// cancellation.
func TestModelSyncOwnsInflightQueryAndInput(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	completed := make(chan error, 1)
	core := NewModelSyncService(func(ctx context.Context, v *Record) ([]string, error) {
		v.Credentials["access_token"] = "changed-by-adapter"
		close(entered)
		<-release
		return nil, ctx.Err()
	})
	input := &Record{ID: 1, Credentials: map[string]any{"access_token": "original"}}
	// Keep the adapter in flight until stop has exhausted its budget. This
	// exposes both copied input ownership and the canceled context observed
	// when the deliberately blocked adapter finally returns.
	go func() {
		_, err := core.Query(context.Background(), input)
		completed <- err
	}()
	<-entered
	require.Equal(t, "original", input.Credentials["access_token"])
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	stopErr := core.StopContext(ctx)
	require.ErrorIs(t, stopErr, context.DeadlineExceeded)
	_, err := core.Query(context.Background(), input)
	require.ErrorIs(t, err, ErrRefreshStopped)
	close(release)
	require.ErrorIs(t, <-completed, context.Canceled)
	require.Equal(t, stopErr, core.StopContext(context.Background()))
}

// TestModelSyncQueryPairsSuggestions installs a suggestion hook that reads the
// queried record and models. The result carries the hook's map beside an
// independently owned model slice, and a nil hook leaves suggestions absent.
func TestModelSyncQueryPairsSuggestions(t *testing.T) {
	models := []string{"first", "second"}
	svc := NewModelSyncService(func(context.Context, *Record) ([]string, error) { return models, nil })
	result, err := svc.Query(context.Background(), &Record{Platform: PlatformOpenCode})
	require.NoError(t, err)
	require.Nil(t, result.ModelProtocols)
	svc.Suggest = func(value *Record, ids []string) map[string]string {
		require.Equal(t, PlatformOpenCode, value.Platform)
		require.Equal(t, models, ids)
		return map[string]string{"first": "openai_responses"}
	}
	result, err = svc.Query(context.Background(), &Record{Platform: PlatformOpenCode})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"first": "openai_responses"}, result.ModelProtocols)
	result.Models[0] = "changed"
	require.Equal(t, "first", models[0])
}

// TestModelSyncQuerySerializesSuggestions distinguishes unavailable suggestions
// from an authoritative empty table after querying a model. JSON consumers must
// receive an empty object to clear stale accepted routes, while absent hooks or
// unsupported platforms leave the field absent.
func TestModelSyncQuerySerializesSuggestions(t *testing.T) {
	for _, tc := range []struct {
		name        string                                    // query result scenario
		platform    string                                    // queried provider platform
		suggest     func(*Record, []string) map[string]string // optional catalogue lookup
		wantPresent bool                                      // JSON suggestion field presence
		want        map[string]string                         // serialized protocol suggestions
	}{
		{name: "no hook", platform: PlatformOpenCode},
		{
			name: "unsupported platform", platform: PlatformOpenAI,
			suggest: OpenCodeProtocolSuggestions(nil),
		},
		{
			name: "empty publication", platform: PlatformOpenCode, wantPresent: true,
			suggest: OpenCodeProtocolSuggestions(func(string) map[string]string { return map[string]string{} }),
			want:    map[string]string{},
		},
		{
			name: "recognized model", platform: PlatformOpenCode, wantPresent: true,
			suggest: OpenCodeProtocolSuggestions(func(string) map[string]string {
				return map[string]string{"custom-model": "anthropic_messages"}
			}),
			want: map[string]string{"custom-model": "anthropic_messages"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewModelSyncService(func(context.Context, *Record) ([]string, error) {
				return []string{"custom-model"}, nil
			})
			svc.Suggest = tc.suggest
			result, err := svc.Query(t.Context(), &Record{
				Platform: tc.platform,
				Credentials: map[string]any{
					OpenCodeCatalogProtocolsKey: map[string]any{
						"payg": map[string]any{"custom-model": "openai_responses"},
					},
				},
			})
			require.NoError(t, err)
			body, err := json.Marshal(result)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &fields))
			encoded, present := fields["model_protocols"]
			require.Equal(t, tc.wantPresent, present, string(body))
			if !present {
				return
			}
			var protocols map[string]string
			require.NoError(t, json.Unmarshal(encoded, &protocols))
			require.Equal(t, tc.want, protocols)
		})
	}
}
