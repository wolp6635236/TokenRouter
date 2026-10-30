package upstream

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// initialOutputSink supplies previously committed metadata and controlled emit failures.
type initialOutputSink struct {
	head       OutputHead // initial response commitment
	emitErr    error      // injected emission failure
	stateReads int        // live commitment query count
}

// InitialOutput returns initial headers and both independent commitment observations.
func (s *initialOutputSink) InitialOutput() OutputHead { return s.head }

// OutputState returns commitment without accessing or initializing output headers.
func (s *initialOutputSink) OutputState() OutputHead { s.stateReads++; return s.head }

// Begin accepts response metadata without changing the fixture's initial observations.
func (s *initialOutputSink) Begin(OutputHead) error { return nil }

// Emit returns the configured failure for each synchronous emission attempt.
func (s *initialOutputSink) Emit(OutputEvent) error { return s.emitErr }

// TestOutputContentCommitment starts codecs after heartbeat-only HTTP commitment.
// Both eager and deferred writers must keep recovery possible until a nonempty
// retry-marked payload is successfully emitted to their output sink.
func TestOutputContentCommitment(t *testing.T) {
	for _, constructor := range []struct {
		name       string                          // initialization scenario label
		newContext func(OutputSink) *OutputContext // output initialization strategy
	}{{"eager", NewOutputContext}, {"deferred", NewDeferredOutputContext}} {
		t.Run(constructor.name, func(t *testing.T) {
			sink := &initialOutputSink{head: OutputHead{Committed: true, Status: http.StatusOK}}
			out := constructor.newContext(sink)
			require.True(t, out.Writer.Written())
			require.False(t, out.Writer.ContentWritten())
			out.Writer.Flush()
			require.False(t, out.Writer.ContentWritten())
			_, err := out.Writer.Write(nil)
			require.NoError(t, err)
			require.False(t, out.Writer.ContentWritten())
			_, err = out.Writer.Write([]byte("data: content\n\n"))
			require.NoError(t, err)
			require.True(t, out.Writer.ContentWritten())
		})
	}
}

// TestOutputContentCommitmentRequiresAcceptedRetryPayload checks explicit heartbeat
// flags and failed emissions. Neither may close the retry window, while an
// already committed body must retain its initial content observation.
func TestOutputContentCommitmentRequiresAcceptedRetryPayload(t *testing.T) {
	sink := &initialOutputSink{}
	out := NewOutputContext(sink)
	writer, ok := out.Writer.(*sinkWriter)
	require.True(t, ok)
	writer.next = &OutputEvent{CommitForRetry: false}
	_, err := out.Writer.Write([]byte(": heartbeat\n\n"))
	require.NoError(t, err)
	require.False(t, out.Writer.ContentWritten())
	sink.emitErr = errors.New("disconnected")
	_, err = out.Writer.Write([]byte("content"))
	require.ErrorIs(t, err, sink.emitErr)
	require.False(t, out.Writer.ContentWritten())
	sink.head.ContentCommitted = true
	require.True(t, NewOutputContext(sink).Writer.ContentWritten())
	require.True(t, NewDeferredOutputContext(sink).Writer.ContentWritten())
}

// TestOutputWriterCachesHTTPCommitment changes live sink state after codec
// creation. Once commitment becomes true, later observations must retain it
// without repeatedly traversing the sink's synchronized state accessors.
func TestOutputWriterCachesHTTPCommitment(t *testing.T) {
	sink := &initialOutputSink{}
	output := NewOutputContext(sink)
	require.False(t, output.Writer.Written())
	require.Equal(t, 1, sink.stateReads)
	sink.head.Committed = true
	require.True(t, output.Writer.Written())
	require.Equal(t, 2, sink.stateReads)
	for range 3 {
		require.True(t, output.Writer.Written())
	}
	require.Equal(t, 2, sink.stateReads)
	require.False(t, output.Writer.ContentWritten())
}
