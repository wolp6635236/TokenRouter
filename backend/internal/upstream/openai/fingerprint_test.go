package openai

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

const fingerprintOriginalSeed = "11111111-1111-4111-8111-111111111111"

func TestResolveConvergedThreadID_PerClientSession(t *testing.T) {
	a := ResolveConvergedThreadID(fingerprintOriginalSeed, "session-aaa")
	b := ResolveConvergedThreadID(fingerprintOriginalSeed, "session-bbb")
	assert.NotEqual(t, a, b, "不同客户端 session 应得到不同 thread_id")
}

func TestResolveConvergedThreadID_Deterministic(t *testing.T) {
	a := ResolveConvergedThreadID(fingerprintOriginalSeed, "session-aaa")
	b := ResolveConvergedThreadID(fingerprintOriginalSeed, "session-aaa")
	assert.Equal(t, a, b, "同一客户端 session 应得到相同 thread_id")
}

func TestResolveConvergedThreadID_EmptySession(t *testing.T) {
	assert.Equal(t, "", ResolveConvergedThreadID(fingerprintOriginalSeed, ""))
}

func TestExtractClientSessionID(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		expected string
	}{
		{"连字符形式优先", func() http.Header {
			h := http.Header{}
			h.Set("session-id", "hyphen-form")
			h.Set("session_id", "underscore-form")
			return h
		}(), "hyphen-form"},
		{"回退到下划线形式", func() http.Header {
			h := http.Header{}
			h.Set("session_id", "underscore-form")
			return h
		}(), "underscore-form"},
		{"都没有", http.Header{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ExtractClientSessionID(tt.headers))
		})
	}
}
