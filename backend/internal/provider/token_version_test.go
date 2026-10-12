package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// tokenVersionReader 返回版本复核使用的提供商记录。
type tokenVersionReader struct {
	value *Record
	err   error
}

func TestCheckTokenVersion(t *testing.T) {
	tests := []struct {
		name           string
		provider       *Record
		latestProvider *Record
		repoErr        error
		expectedStale  bool
	}{
		{
			name:           "nil_provider",
			provider:       nil,
			latestProvider: nil,
			expectedStale:  false,
		},
		{
			name: "no_version_in_provider_but_db_has_version",
			provider: &Record{
				ID:          1,
				Credentials: map[string]any{},
			},
			latestProvider: &Record{
				ID:          1,
				Credentials: map[string]any{"_token_version": int64(100)},
			},
			expectedStale: true, // 当前 provider 无版本但 DB 有，说明已被异步刷新，当前已过时
		},
		{
			name: "both_no_version",
			provider: &Record{
				ID:          1,
				Credentials: map[string]any{},
			},
			latestProvider: &Record{
				ID:          1,
				Credentials: map[string]any{},
			},
			expectedStale: false, // 两边都没有版本号，说明从未被异步刷新过，允许缓存
		},
		{
			name: "same_version",
			provider: &Record{
				ID:          1,
				Credentials: map[string]any{"_token_version": int64(100)},
			},
			latestProvider: &Record{
				ID:          1,
				Credentials: map[string]any{"_token_version": int64(100)},
			},
			expectedStale: false,
		},
		{
			name: "current_version_newer",
			provider: &Record{
				ID:          1,
				Credentials: map[string]any{"_token_version": int64(200)},
			},
			latestProvider: &Record{
				ID:          1,
				Credentials: map[string]any{"_token_version": int64(100)},
			},
			expectedStale: false,
		},
		{
			name: "current_version_older_stale",
			provider: &Record{
				ID:          1,
				Credentials: map[string]any{"_token_version": int64(100)},
			},
			latestProvider: &Record{
				ID:          1,
				Credentials: map[string]any{"_token_version": int64(200)},
			},
			expectedStale: true, // 当前版本过时
		},
		{
			name: "repo_error",
			provider: &Record{
				ID:          1,
				Credentials: map[string]any{"_token_version": int64(100)},
			},
			latestProvider: nil,
			repoErr:        errors.New("db error"),
			expectedStale:  false, // 查询失败，默认允许缓存
		},
		{
			name: "repo_returns_nil",
			provider: &Record{
				ID:          1,
				Credentials: map[string]any{"_token_version": int64(100)},
			},
			latestProvider: nil,
			repoErr:        nil,
			expectedStale:  false, // 查询返回 nil，默认允许缓存
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 表格测试直接调用 token 版本比较实现。
			repo := tokenVersionReader{value: tt.latestProvider, err: tt.repoErr}
			_, isStale := CheckTokenVersion(context.Background(), tt.provider, repo)
			require.Equal(t, tt.expectedStale, isStale)
		})
	}
}

func TestCheckTokenVersion_NilRepo(t *testing.T) {
	provider := &Record{
		ID:          1,
		Credentials: map[string]any{"_token_version": int64(100)},
	}
	_, isStale := CheckTokenVersion(context.Background(), provider, nil)
	require.False(t, isStale) // nil repo，默认允许缓存
}

func (r tokenVersionReader) GetByID(context.Context, int64) (*Record, error) {
	return r.value, r.err
}
