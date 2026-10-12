package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
)

func TestBuildSelectedSet(t *testing.T) {
	tests := []struct {
		name     string
		ids      []string
		wantNil  bool
		wantSize int
	}{
		{
			name:    "nil input returns nil (backward compatible: create all)",
			ids:     nil,
			wantNil: true,
		},
		{
			name:     "empty slice returns empty map (create none)",
			ids:      []string{},
			wantNil:  false,
			wantSize: 0,
		},
		{
			name:     "single ID",
			ids:      []string{"abc-123"},
			wantNil:  false,
			wantSize: 1,
		},
		{
			name:     "multiple IDs",
			ids:      []string{"a", "b", "c"},
			wantNil:  false,
			wantSize: 3,
		},
		{
			name:     "duplicate IDs are deduplicated",
			ids:      []string{"a", "a", "b"},
			wantNil:  false,
			wantSize: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CRSBuildSelectedSet(tt.ids)
			if tt.wantNil {
				if got != nil {
					t.Errorf("buildSelectedSet(%v) = %v, want nil", tt.ids, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("buildSelectedSet(%v) = nil, want non-nil map", tt.ids)
			}
			if len(got) != tt.wantSize {
				t.Errorf("buildSelectedSet(%v) has %d entries, want %d", tt.ids, len(got), tt.wantSize)
			}
			// 检查结果包含全部去重后的 ID。
			for _, id := range tt.ids {
				if _, ok := got[id]; !ok {
					t.Errorf("buildSelectedSet(%v) missing key %q", tt.ids, id)
				}
			}
		})
	}
}

func TestShouldCreateProvider(t *testing.T) {
	tests := []struct {
		name        string
		crsID       string
		selectedSet map[string]struct{}
		want        bool
	}{
		{
			name:        "nil set allows all (backward compatible)",
			crsID:       "any-id",
			selectedSet: nil,
			want:        true,
		},
		{
			name:        "empty set blocks all",
			crsID:       "any-id",
			selectedSet: map[string]struct{}{},
			want:        false,
		},
		{
			name:        "ID in set is allowed",
			crsID:       "abc-123",
			selectedSet: map[string]struct{}{"abc-123": {}, "def-456": {}},
			want:        true,
		},
		{
			name:        "ID not in set is blocked",
			crsID:       "xyz-789",
			selectedSet: map[string]struct{}{"abc-123": {}, "def-456": {}},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CRSShouldCreateProvider(tt.crsID, tt.selectedSet)
			if got != tt.want {
				t.Errorf("shouldCreateProvider(%q, %v) = %v, want %v",
					tt.crsID, tt.selectedSet, got, tt.want)
			}
		})
	}
}

// TestGuardCRSShadowParentInvariant 有 spark 影子的母提供商经 CRS 任意分支更新后,目标结果
// 必须仍是 OpenAI OAuth;否则(改 api_key 或跨平台 Anthropic/Gemini)影子读透母凭据失败、spark 全崩。
func TestGuardCRSShadowParentInvariant(t *testing.T) {
	ctx := context.Background()
	repo := newCRSShadowStore()

	mother := &Record{Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth}
	require.NoError(t, repo.Create(ctx, mother))
	parentID := mother.ID

	// 无影子:任何目标都放行(含改离 OpenAI OAuth)。
	require.NoError(t, GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformOpenAI, capability.ProviderTypeAPIKey))
	require.NoError(t, GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformAnthropic, capability.ProviderTypeOAuth))

	// 建一个影子后:
	shadow := &Record{
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		ParentProviderID: &parentID,
		QuotaDimension:   QuotaDimensionSpark,
	}
	require.NoError(t, repo.Create(ctx, shadow))

	// 翻成 OpenAI api_key 被拒。
	err := GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformOpenAI, capability.ProviderTypeAPIKey)
	require.Error(t, err, "must reject converting a shadow parent to openai api_key")
	require.Contains(t, err.Error(), "spark-shadow parent")

	// 跨平台改成 Anthropic OAuth(Type 仍 OAuth、仅 Platform 变)也被拒，验证平台和类型必须同时满足约束。
	require.Error(t, GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformAnthropic, capability.ProviderTypeOAuth),
		"must reject moving a shadow parent to a non-OpenAI platform even if type stays oauth")

	// 改成 Gemini api_key 被拒。
	require.Error(t, GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformGemini, capability.ProviderTypeAPIKey))

	// 保持 OpenAI OAuth(重新同步母提供商)放行,即便仍有影子。
	require.NoError(t, GuardCRSShadowParentInvariant(ctx, repo, mother, capability.PlatformOpenAI, capability.ProviderTypeOAuth))
}
