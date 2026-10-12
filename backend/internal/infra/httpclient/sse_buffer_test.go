package httpclient

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSSEScannerBuf64KPool_GetPutDoesNotPanic(t *testing.T) {
	buf := GetSSEScannerBuf64K()
	require.NotNil(t, buf)
	require.Equal(t, SSEScannerBuf64KSize, len(buf[:]))

	buf[0] = 1
	PutSSEScannerBuf64K(buf)

	// nil 缓冲区可直接归还。
	PutSSEScannerBuf64K(nil)
}
