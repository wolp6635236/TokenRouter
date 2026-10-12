package provider

// 本文件检查父包 ../execution_values.go 的输出归一化和 ../execution_error.go 的重试错误分类。

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/creative"
)

// TestNormalizeCreativeOutputs 校验大小上限、去重并固定只保留一张。
func TestNormalizeCreativeOutputs(t *testing.T) {
	// sha256 去重：相同字节只保留一张。
	outputs, err := creative.NormalizeCreativeOutputs([]creative.CreativeOutput{
		{Index: 0, Bytes: []byte("same"), Mime: "image/png"},
		{Index: 1, Bytes: []byte("same"), Mime: "image/png"},
		{Index: 2, Bytes: []byte("other"), Mime: "image/png"},
	})
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	require.Equal(t, []byte("same"), outputs[0].Bytes)

	// 上游返回多张时固定截断为一张并重排行号。
	outputs, err = creative.NormalizeCreativeOutputs([]creative.CreativeOutput{
		{Index: 0, Bytes: []byte("a"), Mime: "image/png"},
		{Index: 1, Bytes: []byte("b"), Mime: "image/png"},
		{Index: 2, Bytes: []byte("c"), Mime: "image/png"},
	})
	require.NoError(t, err)
	require.Len(t, outputs, 1)
	require.Equal(t, 0, outputs[0].Index)

	// 单张超限（>32MiB）视为失败。
	big := make([]byte, creative.CreativeMaxOutputBytes+1)
	_, err = creative.NormalizeCreativeOutputs([]creative.CreativeOutput{{Index: 0, Bytes: big, Mime: "image/png"}})
	require.Error(t, err)
	require.False(t, creative.IsRetryableCreativeError(err))

	// 空输出视为失败。
	_, err = creative.NormalizeCreativeOutputs(nil)
	require.Error(t, err)
}

// TestCreativeErrorRetryableMatrix 校验状态码到可重试性的映射。
func TestCreativeErrorRetryableMatrix(t *testing.T) {
	retryable := []int{0, 429, 500, 502, 503}
	for _, status := range retryable {
		err := creative.CreativeHTTPStatusError(status, "boom")
		require.True(t, creative.IsRetryableCreativeError(err), "status %d 应当可重试", status)
	}
	nonRetryable := []int{400, 401, 403, 404, 422}
	for _, status := range nonRetryable {
		err := creative.CreativeHTTPStatusError(status, "bad request")
		require.False(t, creative.IsRetryableCreativeError(err), "status %d 应当不可重试", status)
	}
	require.False(t, creative.IsRetryableCreativeError(nil))
	require.True(t, creative.IsRetryableCreativeError(errors.New("network down")))
	require.True(t, creative.IsRetryableCreativeError(creative.CreativeNonRetryableError("x")) == false)
}
