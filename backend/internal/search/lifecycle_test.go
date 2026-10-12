package search

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSearchStopWaitsAndRejectsNewWork(t *testing.T) {
	group := NewWorkGroup()
	done, err := group.Begin()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	require.ErrorIs(t, group.Stop(ctx), context.DeadlineExceeded)
	_, err = group.Begin()
	require.Error(t, err)
	done()
	done()
	require.NoError(t, group.Stop(context.Background()))
}
