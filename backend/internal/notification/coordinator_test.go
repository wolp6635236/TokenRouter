package notification

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNotificationLockCancellationAndIsolation(t *testing.T) {
	var locks keyCoordinator
	release, err := locks.acquire(context.Background(), "one")
	require.NoError(t, err)
	other, err := locks.acquire(context.Background(), "two")
	require.NoError(t, err)
	other()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := locks.acquire(ctx, "one"); done <- err }()
	require.Eventually(t, func() bool { locks.mu.Lock(); defer locks.mu.Unlock(); return locks.entries["one"].refs == 2 }, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	release()
	release()
	require.Empty(t, locks.entries)
}
