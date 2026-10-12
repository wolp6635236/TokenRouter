package provider

import (
	"testing"
)

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
		{name: "google_one_free", raw: "google_one_free", want: GeminiTierGoogleOneFree},
		{name: "google_ai_pro", raw: "google_ai_pro", want: GeminiTierGoogleAIPro},
		{name: "google_ai_ultra", raw: "google_ai_ultra", want: GeminiTierGoogleAIUltra},
		{name: "gcp_standard", raw: "gcp_standard", want: GeminiTierGCPStandard},
		{name: "gcp_enterprise", raw: "gcp_enterprise", want: GeminiTierGCPEnterprise},
		{name: "aistudio_free", raw: "aistudio_free", want: GeminiTierAIStudioFree},
		{name: "aistudio_paid", raw: "aistudio_paid", want: GeminiTierAIStudioPaid},
		{name: "google_one_unknown", raw: "google_one_unknown", want: GeminiTierGoogleOneUnknown},

		// 大小写不敏感
		{name: "Google_One_Free 大写", raw: "Google_One_Free", want: GeminiTierGoogleOneFree},
		{name: "GCP_STANDARD 全大写", raw: "GCP_STANDARD", want: GeminiTierGCPStandard},

		// legacy 映射: Google One
		{name: "AI_PREMIUM -> google_ai_pro", raw: "AI_PREMIUM", want: GeminiTierGoogleAIPro},
		{name: "FREE -> google_one_free", raw: "FREE", want: GeminiTierGoogleOneFree},
		{name: "GOOGLE_ONE_BASIC -> google_one_free", raw: "GOOGLE_ONE_BASIC", want: GeminiTierGoogleOneFree},
		{name: "GOOGLE_ONE_STANDARD -> google_one_free", raw: "GOOGLE_ONE_STANDARD", want: GeminiTierGoogleOneFree},
		{name: "GOOGLE_ONE_UNLIMITED -> google_ai_ultra", raw: "GOOGLE_ONE_UNLIMITED", want: GeminiTierGoogleAIUltra},
		{name: "GOOGLE_ONE_UNKNOWN -> google_one_unknown", raw: "GOOGLE_ONE_UNKNOWN", want: GeminiTierGoogleOneUnknown},

		// legacy 映射: Code Assist
		{name: "STANDARD -> gcp_standard", raw: "STANDARD", want: GeminiTierGCPStandard},
		{name: "PRO -> gcp_standard", raw: "PRO", want: GeminiTierGCPStandard},
		{name: "LEGACY -> gcp_standard", raw: "LEGACY", want: GeminiTierGCPStandard},
		{name: "ENTERPRISE -> gcp_enterprise", raw: "ENTERPRISE", want: GeminiTierGCPEnterprise},
		{name: "ULTRA -> gcp_enterprise", raw: "ULTRA", want: GeminiTierGCPEnterprise},

		// 连字符命名。
		{name: "standard-tier -> gcp_standard", raw: "standard-tier", want: GeminiTierGCPStandard},
		{name: "pro-tier -> gcp_standard", raw: "pro-tier", want: GeminiTierGCPStandard},
		{name: "ultra-tier -> gcp_enterprise", raw: "ultra-tier", want: GeminiTierGCPEnterprise},

		// 未知值
		{name: "unknown_value -> 空", raw: "unknown_value", want: ""},
		{name: "random-text -> 空", raw: "random-text", want: ""},

		// 带空白
		{name: "带前后空白", raw: "  google_one_free  ", want: GeminiTierGoogleOneFree},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CanonicalGeminiTierID(tt.raw)
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
		{name: "google_one + google_one_free", oauthType: "google_one", tierID: "google_one_free", want: GeminiTierGoogleOneFree},
		{name: "google_one + google_ai_pro", oauthType: "google_one", tierID: "google_ai_pro", want: GeminiTierGoogleAIPro},
		{name: "google_one + google_ai_ultra", oauthType: "google_one", tierID: "google_ai_ultra", want: GeminiTierGoogleAIUltra},
		{name: "google_one + gcp_standard 被过滤", oauthType: "google_one", tierID: "gcp_standard", want: ""},
		{name: "google_one + aistudio_free 被过滤", oauthType: "google_one", tierID: "aistudio_free", want: ""},
		{name: "google_one + AI_PREMIUM 遗留映射", oauthType: "google_one", tierID: "AI_PREMIUM", want: GeminiTierGoogleAIPro},

		// code_assist 类型过滤
		{name: "code_assist + gcp_standard", oauthType: "code_assist", tierID: "gcp_standard", want: GeminiTierGCPStandard},
		{name: "code_assist + gcp_enterprise", oauthType: "code_assist", tierID: "gcp_enterprise", want: GeminiTierGCPEnterprise},
		{name: "code_assist + google_one_free 被过滤", oauthType: "code_assist", tierID: "google_one_free", want: ""},
		{name: "code_assist + aistudio_free 被过滤", oauthType: "code_assist", tierID: "aistudio_free", want: ""},
		{name: "code_assist + STANDARD 遗留映射", oauthType: "code_assist", tierID: "STANDARD", want: GeminiTierGCPStandard},
		{name: "code_assist + standard-tier kebab", oauthType: "code_assist", tierID: "standard-tier", want: GeminiTierGCPStandard},

		// ai_studio 类型过滤
		{name: "ai_studio + aistudio_free", oauthType: "ai_studio", tierID: "aistudio_free", want: GeminiTierAIStudioFree},
		{name: "ai_studio + aistudio_paid", oauthType: "ai_studio", tierID: "aistudio_paid", want: GeminiTierAIStudioPaid},
		{name: "ai_studio + gcp_standard 被过滤", oauthType: "ai_studio", tierID: "gcp_standard", want: ""},
		{name: "ai_studio + google_one_free 被过滤", oauthType: "ai_studio", tierID: "google_one_free", want: ""},

		// 空值
		{name: "空 tierID", oauthType: "google_one", tierID: "", want: ""},
		{name: "空 oauthType + 有效 tierID", oauthType: "", tierID: "gcp_standard", want: GeminiTierGCPStandard},
		{name: "未知 oauthType 接受规范化值", oauthType: "unknown_type", tierID: "gcp_standard", want: GeminiTierGCPStandard},

		// oauthType 大小写和空白
		{name: "GOOGLE_ONE 大写", oauthType: "GOOGLE_ONE", tierID: "google_one_free", want: GeminiTierGoogleOneFree},
		{name: "oauthType 带空白", oauthType: "  code_assist  ", tierID: "gcp_standard", want: GeminiTierGCPStandard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CanonicalGeminiTierIDForOAuthType(tt.oauthType, tt.tierID)
			if got != tt.want {
				t.Fatalf("providercore.CanonicalGeminiTierIDForOAuthType(%q, %q) = %q, want %q", tt.oauthType, tt.tierID, got, tt.want)
			}
		})
	}
}
