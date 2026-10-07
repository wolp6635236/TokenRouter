package provider_test

import (
	"fmt"
	"strings"
	"testing"

	providercore "github.com/TokenFlux/TokenRouter/internal/provider"

	geminicli "github.com/TokenFlux/TokenRouter/internal/protocol/gemini"
)

func TestValidateTierID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tierID  string
		wantErr bool
	}{
		{name: "空字符串合法", tierID: "", wantErr: false},
		{name: "正常 tier_id", tierID: "google_one_free", wantErr: false},
		{name: "包含斜杠", tierID: "tier/sub", wantErr: false},
		{name: "包含连字符", tierID: "gcp-standard", wantErr: false},
		{name: "纯数字", tierID: "12345", wantErr: false},
		{name: "超长字符串（65个字符）", tierID: strings.Repeat("a", 65), wantErr: true},
		{name: "刚好64个字符", tierID: strings.Repeat("b", 64), wantErr: false},
		{name: "非法字符_空格", tierID: "tier id", wantErr: true},
		{name: "非法字符_中文", tierID: "tier_中文", wantErr: true},
		{name: "非法字符_特殊符号", tierID: "tier@id", wantErr: true},
		{name: "非法字符_感叹号", tierID: "tier!id", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := providercore.ValidateTierID(tt.tierID)
			if tt.wantErr && err == nil {
				t.Fatalf("期望返回错误，但返回 nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("不期望返回错误，但返回: %v", err)
			}
		})
	}
}

func TestCanonicalGeminiTierID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		// 空值
		{name: "空字符串", raw: "", want: ""},
		{name: "纯空白", raw: "   ", want: ""},

		// 已规范化的值（直接返回）
		{name: "google_one_free", raw: "google_one_free", want: providercore.GeminiTierGoogleOneFree},
		{name: "google_ai_pro", raw: "google_ai_pro", want: providercore.GeminiTierGoogleAIPro},
		{name: "google_ai_ultra", raw: "google_ai_ultra", want: providercore.GeminiTierGoogleAIUltra},
		{name: "gcp_standard", raw: "gcp_standard", want: providercore.GeminiTierGCPStandard},
		{name: "gcp_enterprise", raw: "gcp_enterprise", want: providercore.GeminiTierGCPEnterprise},
		{name: "aistudio_free", raw: "aistudio_free", want: providercore.GeminiTierAIStudioFree},
		{name: "aistudio_paid", raw: "aistudio_paid", want: providercore.GeminiTierAIStudioPaid},
		{name: "google_one_unknown", raw: "google_one_unknown", want: providercore.GeminiTierGoogleOneUnknown},

		// 大小写不敏感
		{name: "Google_One_Free 大写", raw: "Google_One_Free", want: providercore.GeminiTierGoogleOneFree},
		{name: "GCP_STANDARD 全大写", raw: "GCP_STANDARD", want: providercore.GeminiTierGCPStandard},

		// legacy 映射: Google One
		{name: "AI_PREMIUM -> google_ai_pro", raw: "AI_PREMIUM", want: providercore.GeminiTierGoogleAIPro},
		{name: "FREE -> google_one_free", raw: "FREE", want: providercore.GeminiTierGoogleOneFree},
		{name: "GOOGLE_ONE_BASIC -> google_one_free", raw: "GOOGLE_ONE_BASIC", want: providercore.GeminiTierGoogleOneFree},
		{name: "GOOGLE_ONE_STANDARD -> google_one_free", raw: "GOOGLE_ONE_STANDARD", want: providercore.GeminiTierGoogleOneFree},
		{name: "GOOGLE_ONE_UNLIMITED -> google_ai_ultra", raw: "GOOGLE_ONE_UNLIMITED", want: providercore.GeminiTierGoogleAIUltra},
		{name: "GOOGLE_ONE_UNKNOWN -> google_one_unknown", raw: "GOOGLE_ONE_UNKNOWN", want: providercore.GeminiTierGoogleOneUnknown},

		// legacy 映射: Code Assist
		{name: "STANDARD -> gcp_standard", raw: "STANDARD", want: providercore.GeminiTierGCPStandard},
		{name: "PRO -> gcp_standard", raw: "PRO", want: providercore.GeminiTierGCPStandard},
		{name: "LEGACY -> gcp_standard", raw: "LEGACY", want: providercore.GeminiTierGCPStandard},
		{name: "ENTERPRISE -> gcp_enterprise", raw: "ENTERPRISE", want: providercore.GeminiTierGCPEnterprise},
		{name: "ULTRA -> gcp_enterprise", raw: "ULTRA", want: providercore.GeminiTierGCPEnterprise},

		// kebab-case
		{name: "standard-tier -> gcp_standard", raw: "standard-tier", want: providercore.GeminiTierGCPStandard},
		{name: "pro-tier -> gcp_standard", raw: "pro-tier", want: providercore.GeminiTierGCPStandard},
		{name: "ultra-tier -> gcp_enterprise", raw: "ultra-tier", want: providercore.GeminiTierGCPEnterprise},

		// 未知值
		{name: "unknown_value -> 空", raw: "unknown_value", want: ""},
		{name: "random-text -> 空", raw: "random-text", want: ""},

		// 带空白
		{name: "带前后空白", raw: "  google_one_free  ", want: providercore.GeminiTierGoogleOneFree},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := providercore.CanonicalGeminiTierID(tt.raw)
			if got != tt.want {
				t.Fatalf("providercore.CanonicalGeminiTierID(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCanonicalGeminiTierIDForOAuthType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		oauthType string
		tierID    string
		want      string
	}{
		// google_one 类型过滤
		{name: "google_one + google_one_free", oauthType: "google_one", tierID: "google_one_free", want: providercore.GeminiTierGoogleOneFree},
		{name: "google_one + google_ai_pro", oauthType: "google_one", tierID: "google_ai_pro", want: providercore.GeminiTierGoogleAIPro},
		{name: "google_one + google_ai_ultra", oauthType: "google_one", tierID: "google_ai_ultra", want: providercore.GeminiTierGoogleAIUltra},
		{name: "google_one + gcp_standard 被过滤", oauthType: "google_one", tierID: "gcp_standard", want: ""},
		{name: "google_one + aistudio_free 被过滤", oauthType: "google_one", tierID: "aistudio_free", want: ""},
		{name: "google_one + AI_PREMIUM 遗留映射", oauthType: "google_one", tierID: "AI_PREMIUM", want: providercore.GeminiTierGoogleAIPro},

		// code_assist 类型过滤
		{name: "code_assist + gcp_standard", oauthType: "code_assist", tierID: "gcp_standard", want: providercore.GeminiTierGCPStandard},
		{name: "code_assist + gcp_enterprise", oauthType: "code_assist", tierID: "gcp_enterprise", want: providercore.GeminiTierGCPEnterprise},
		{name: "code_assist + google_one_free 被过滤", oauthType: "code_assist", tierID: "google_one_free", want: ""},
		{name: "code_assist + aistudio_free 被过滤", oauthType: "code_assist", tierID: "aistudio_free", want: ""},
		{name: "code_assist + STANDARD 遗留映射", oauthType: "code_assist", tierID: "STANDARD", want: providercore.GeminiTierGCPStandard},
		{name: "code_assist + standard-tier kebab", oauthType: "code_assist", tierID: "standard-tier", want: providercore.GeminiTierGCPStandard},

		// ai_studio 类型过滤
		{name: "ai_studio + aistudio_free", oauthType: "ai_studio", tierID: "aistudio_free", want: providercore.GeminiTierAIStudioFree},
		{name: "ai_studio + aistudio_paid", oauthType: "ai_studio", tierID: "aistudio_paid", want: providercore.GeminiTierAIStudioPaid},
		{name: "ai_studio + gcp_standard 被过滤", oauthType: "ai_studio", tierID: "gcp_standard", want: ""},
		{name: "ai_studio + google_one_free 被过滤", oauthType: "ai_studio", tierID: "google_one_free", want: ""},

		// 空值
		{name: "空 tierID", oauthType: "google_one", tierID: "", want: ""},
		{name: "空 oauthType + 有效 tierID", oauthType: "", tierID: "gcp_standard", want: providercore.GeminiTierGCPStandard},
		{name: "未知 oauthType 接受规范化值", oauthType: "unknown_type", tierID: "gcp_standard", want: providercore.GeminiTierGCPStandard},

		// oauthType 大小写和空白
		{name: "GOOGLE_ONE 大写", oauthType: "GOOGLE_ONE", tierID: "google_one_free", want: providercore.GeminiTierGoogleOneFree},
		{name: "oauthType 带空白", oauthType: "  code_assist  ", tierID: "gcp_standard", want: providercore.GeminiTierGCPStandard},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := providercore.CanonicalGeminiTierIDForOAuthType(tt.oauthType, tt.tierID)
			if got != tt.want {
				t.Fatalf("providercore.CanonicalGeminiTierIDForOAuthType(%q, %q) = %q, want %q", tt.oauthType, tt.tierID, got, tt.want)
			}
		})
	}
}

func TestExtractTierIDFromAllowedTiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		allowedTiers []geminicli.AllowedTier
		want         string
	}{
		{
			name:         "nil 列表返回 LEGACY",
			allowedTiers: nil,
			want:         "LEGACY",
		},
		{
			name:         "空列表返回 LEGACY",
			allowedTiers: []geminicli.AllowedTier{},
			want:         "LEGACY",
		},
		{
			name: "有 IsDefault 的 tier",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "STANDARD", IsDefault: false},
				{ID: "PRO", IsDefault: true},
				{ID: "ENTERPRISE", IsDefault: false},
			},
			want: "PRO",
		},
		{
			name: "没有 IsDefault 取第一个非空",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "STANDARD", IsDefault: false},
				{ID: "ENTERPRISE", IsDefault: false},
			},
			want: "STANDARD",
		},
		{
			name: "IsDefault 的 ID 为空，取第一个非空",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "", IsDefault: true},
				{ID: "PRO", IsDefault: false},
			},
			want: "PRO",
		},
		{
			name: "所有 ID 都为空返回 LEGACY",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "", IsDefault: false},
				{ID: "   ", IsDefault: false},
			},
			want: "LEGACY",
		},
		{
			name: "ID 带空白会被 trim",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "  STANDARD  ", IsDefault: true},
			},
			want: "STANDARD",
		},
		{
			name: "单个 tier 且 IsDefault",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "ENTERPRISE", IsDefault: true},
			},
			want: "ENTERPRISE",
		},
		{
			name: "单个 tier 非 IsDefault",
			allowedTiers: []geminicli.AllowedTier{
				{ID: "ENTERPRISE", IsDefault: false},
			},
			want: "ENTERPRISE",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := providercore.ExtractTierIDFromAllowedTiers(tt.allowedTiers)
			if got != tt.want {
				t.Fatalf("providercore.ExtractTierIDFromAllowedTiers() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferGoogleOneTier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		storageBytes int64
		want         string
	}{
		// 检查小于等于零的输入。
		{name: "0 bytes -> unknown", storageBytes: 0, want: providercore.GeminiTierGoogleOneUnknown},
		{name: "负数 -> unknown", storageBytes: -1, want: providercore.GeminiTierGoogleOneUnknown},

		// > 100TB -> ultra
		{name: "> 100TB -> ultra", storageBytes: int64(providercore.StorageTierUnlimited) + 1, want: providercore.GeminiTierGoogleAIUltra},
		{name: "200TB -> ultra", storageBytes: 200 * int64(providercore.TB), want: providercore.GeminiTierGoogleAIUltra},

		// >= 2TB -> pro (但 <= 100TB)
		{name: "正好 2TB -> pro", storageBytes: int64(providercore.StorageTierAIPremium), want: providercore.GeminiTierGoogleAIPro},
		{name: "5TB -> pro", storageBytes: 5 * int64(providercore.TB), want: providercore.GeminiTierGoogleAIPro},
		{name: "100TB 正好 -> pro (不是 > 100TB)", storageBytes: int64(providercore.StorageTierUnlimited), want: providercore.GeminiTierGoogleAIPro},

		// >= 15GB -> free (但 < 2TB)
		{name: "正好 15GB -> free", storageBytes: int64(providercore.StorageTierFree), want: providercore.GeminiTierGoogleOneFree},
		{name: "100GB -> free", storageBytes: 100 * int64(providercore.GB), want: providercore.GeminiTierGoogleOneFree},
		{name: "略低于 2TB -> free", storageBytes: int64(providercore.StorageTierAIPremium) - 1, want: providercore.GeminiTierGoogleOneFree},

		// < 15GB -> unknown
		{name: "1GB -> unknown", storageBytes: int64(providercore.GB), want: providercore.GeminiTierGoogleOneUnknown},
		{name: "略低于 15GB -> unknown", storageBytes: int64(providercore.StorageTierFree) - 1, want: providercore.GeminiTierGoogleOneUnknown},
		{name: "1 byte -> unknown", storageBytes: 1, want: providercore.GeminiTierGoogleOneUnknown},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := providercore.InferGoogleOneTier(tt.storageBytes, t.Logf)
			if got != tt.want {
				t.Fatalf("providercore.InferGoogleOneTier(%d) = %q, want %q", tt.storageBytes, got, tt.want)
			}
		})
	}
}

func TestIsNonRetryableGeminiOAuthError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "invalid_grant", err: fmt.Errorf("error: invalid_grant"), want: true},
		{name: "invalid_client", err: fmt.Errorf("oauth error: invalid_client"), want: true},
		{name: "unauthorized_client", err: fmt.Errorf("unauthorized_client: mismatch"), want: true},
		{name: "access_denied", err: fmt.Errorf("access_denied by user"), want: true},
		{name: "普通网络错误", err: fmt.Errorf("connection timeout"), want: false},
		{name: "HTTP 500 错误", err: fmt.Errorf("server error 500"), want: false},
		{name: "空错误信息", err: fmt.Errorf(""), want: false},
		{name: "包含 invalid 但不是完整匹配", err: fmt.Errorf("invalid request"), want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := providercore.IsNonRetryableGeminiOAuthError(tt.err)
			if got != tt.want {
				t.Fatalf("providercore.IsNonRetryableGeminiOAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
