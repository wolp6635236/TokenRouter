package creative_test

import (
	"context"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/creative"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	testassert "github.com/TokenFlux/TokenRouter/internal/testutil/assertion"
	"github.com/stretchr/testify/require"
)

// TestCreativeMixedGroupCatalog 按实际提供商能力提供同组多平台模型，并固化创建时的供应商。
func TestCreativeMixedGroupCatalog(t *testing.T) {
	svc := newCreativeTestService()
	rows := testassert.MustType[*creativeFakeProviderRepo](testassert.MustType[creativeProviderReader](svc.ProviderRepo).source)
	settings := testassert.MustType[*creativeFakeSettingReader](svc.Settings)
	settings.models = nil
	rows.byGroup[12] = nil
	models := map[string]string{creative.PlatformOpenAI: "gpt-image-2", creative.PlatformGemini: "gemini-3.1-flash-image", creative.PlatformGrok: "grok-imagine-image-2.0"}
	for platform, model := range models {
		rows.byGroup[12] = append(rows.byGroup[12], providercore.Record{ID: int64(len(rows.byGroup[12]) + 55), Platform: platform, Type: "apikey", Status: "active", Schedulable: true, Credentials: map[string]any{"model_whitelist": []string{model}}})
		settings.models = append(settings.models, creative.CreativeModelSetting{GroupID: 12, Model: model, Operations: []string{"generate", "edit", "inpaint"}})
	}
	listed, err := svc.ListModels(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, listed.Data, 3)
	for platform, model := range models {
		params := validCreateParams()
		params.Model = model
		validated, err := svc.ValidateCreateParams(context.Background(), 7, &params)
		require.NoError(t, err)
		require.Equal(t, platform, validated.Platform)
		for _, entry := range listed.Data {
			if entry.Model != model {
				continue
			}
			require.Equal(t, platform == creative.PlatformOpenAI, creative.CreativeContainsOption(entry.Operations, "inpaint"))
		}
	}
	// 禁用提供商媒体协议后，模型不能只凭白名单继续出现在目录。
	for i := range rows.byGroup[12] {
		rows.byGroup[12][i].Credentials["upstream_protocols"] = []string{}
	}
	listed, err = svc.ListModels(context.Background(), 7)
	require.NoError(t, err)
	require.Empty(t, listed.Data)
}

// TestCreativeRejectsClientOnlyGroupBeforeFunding 验证专用客户端组不能展示为创作台候选，也不能在预留原组资金后由 worker 改组。
func TestCreativeRejectsClientOnlyGroupBeforeFunding(t *testing.T) {
	svc := newCreativeTestService()
	groups := testassert.MustType[*creativeFakeGroupRepo](testassert.MustType[creativeGroupReader](svc.GroupRepo).source)
	groups.byID[12].ClaudeCodeOnly = true
	fallback := int64(99)
	groups.byID[12].FallbackGroupID = &fallback
	groups.active[0] = *groups.byID[12]
	listed, err := svc.ListModels(context.Background(), 7)
	require.NoError(t, err)
	require.Empty(t, listed.Data)
	candidates, err := svc.ListCreativeModelCandidates(context.Background())
	require.NoError(t, err)
	require.Empty(t, candidates)
	_, err = svc.CreateRun(context.Background(), testCreativeScope(7), validCreateParams(), "client-only")
	require.ErrorIs(t, err, creative.ErrCreativeGroupForbidden)
	require.Zero(t, testassert.MustType[*creativeFakeBillingRepo](creativeFixtureBilling(svc)).reserveN)
}
