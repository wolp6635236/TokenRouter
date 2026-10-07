package batchimage_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/batchimage"
	"github.com/stretchr/testify/require"
)

// TestBatchImageUnboundKeyCannotUseGlobalProviders 确保未绑定 Key 不借用全局提供商池。
func TestBatchImageUnboundKeyCannotUseGlobalProviders(t *testing.T) {
	svc, repo, _, platform, _, _ := newTestBatchImagePublicService(true)
	owner := testBatchImageOwner()
	owner.GroupID = nil
	_, err := svc.Submit(context.Background(), owner, validBatchImageSubmitRequest(), "")
	require.ErrorIs(t, err, batchimage.ErrBatchImageGroupDisabled)
	require.Empty(t, repo.jobs)
	require.Empty(t, platform.submits)
}
