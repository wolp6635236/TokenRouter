package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
)

type mediaFrameStub struct {
	closed int
	order  *[]string
}

type realtimePortsStub struct {
	selected, released, opened, failed int
	denyWait                           bool
	credentialFailure                  bool
	firstDialFailure                   bool
	conn                               *mediaFrameStub
	order                              []string
	deadline                           time.Duration
}

func (f *mediaFrameStub) ReadFrame(context.Context) (upstream.FrameKind, []byte, error) {
	return upstream.FrameText, nil, context.Canceled
}

func (f *mediaFrameStub) WriteFrame(context.Context, upstream.FrameKind, []byte) error { return nil }

func (f *mediaFrameStub) Close() error { f.closed++; *f.order = append(*f.order, "close"); return nil }

func (p *realtimePortsStub) SelectRealtime(_ context.Context, _ map[int64]struct{}) (provider.ProviderSnapshot, bool, error) {
	p.selected++
	return provider.ProviderSnapshot{ID: int64(p.selected)}, true, nil
}

func (p *realtimePortsStub) AcquireRealtime(context.Context, provider.ProviderSnapshot) (func(), bool) {
	if p.denyWait {
		return nil, false
	}
	return func() { p.released++; p.order = append(p.order, "release") }, true
}

func (p *realtimePortsStub) RealtimeCredential(context.Context, provider.ProviderSnapshot) (string, error) {
	if p.credentialFailure {
		return "", errors.New("credential")
	}
	return "secret", nil
}

func (p *realtimePortsStub) OpenRealtime(ctx context.Context, _ provider.ProviderSnapshot, _, _ string) (upstream.FrameConn, error) {
	p.opened++
	deadline, _ := ctx.Deadline()
	p.deadline = time.Until(deadline)
	if p.firstDialFailure && p.opened == 1 {
		return nil, errors.New("dial")
	}
	p.conn = &mediaFrameStub{order: &p.order}
	return p.conn, nil
}

func (p *realtimePortsStub) RealtimeOpenFailed(context.Context, provider.ProviderSnapshot, error) {
	p.failed++
}

func TestRealtimeAdmissionKeepsUpstreamBeforeAcceptAndReleaseOrder(t *testing.T) {
	ports := &realtimePortsStub{firstDialFailure: true}
	result := OpenRealtime(context.Background(), "voice", 12*time.Second, ports)
	require.NotNil(t, result.Lease)
	require.Equal(t, 2, ports.selected)
	require.Equal(t, 1, ports.released)
	require.Equal(t, 1, ports.failed)
	require.Greater(t, ports.deadline, 11*time.Second)
	require.LessOrEqual(t, ports.deadline, 12*time.Second)
	require.NoError(t, result.Lease.Close())
	require.NoError(t, result.Lease.Close())
	require.Equal(t, 1, ports.conn.closed)
	require.Equal(t, 2, ports.released)
	require.Equal(t, []string{"release", "close", "release"}, ports.order)
}

func TestRealtimeAdmissionCredentialFailureAndWaiting(t *testing.T) {
	ports := &realtimePortsStub{credentialFailure: true}
	result := OpenRealtime(context.Background(), "voice", time.Second, ports)
	require.Nil(t, result.Lease)
	require.True(t, result.CandidateSeen)
	require.Equal(t, 4, ports.selected)
	require.Equal(t, 4, ports.released)
	require.Zero(t, ports.opened)
	waiting := &realtimePortsStub{denyWait: true}
	result = OpenRealtime(context.Background(), "voice", time.Second, waiting)
	require.True(t, result.WaitRejected)
	require.Zero(t, waiting.opened)
	require.Zero(t, waiting.released)
}
