package egress

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTLSFingerprintRouterService_ValidateRules(t *testing.T) {
	t.Run("空 pattern 拒绝", func(t *testing.T) {
		router := &TLSFingerprintRouter{
			Name: "invalid",
			Rules: []TLSFingerprintRouterRule{{
				Enabled:   true,
				MatchType: TLSRouterMatchContains,
				Pattern:   "   ",
			}},
		}
		require.Error(t, router.Validate())
	})

	t.Run("非法 regex 拒绝", func(t *testing.T) {
		router := &TLSFingerprintRouter{
			Name: "invalid",
			Rules: []TLSFingerprintRouterRule{{
				Enabled:   true,
				MatchType: TLSRouterMatchRegex,
				Pattern:   "[",
			}},
		}
		require.Error(t, router.Validate())
	})
}

func TestTLSFingerprintRouter_ValidateChatGPTOAuthTokenProfileID(t *testing.T) {
	tests := []struct {
		name    string
		value   *int64
		wantErr bool
	}{
		{name: "未配置", value: nil},
		{name: "内置默认模板", value: tlsRouterInt64Ptr(0)},
		{name: "随机模板", value: tlsRouterInt64Ptr(-1)},
		{name: "指定模板", value: tlsRouterInt64Ptr(123)},
		{name: "非法负数", value: tlsRouterInt64Ptr(-2), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := &TLSFingerprintRouter{
				Name:                                     "router",
				ChatGPTOAuthTokenTLSFingerprintProfileID: tt.value,
			}
			err := router.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func tlsRouterInt64Ptr(value int64) *int64 {
	return &value
}
