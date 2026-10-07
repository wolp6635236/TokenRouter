package site

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/pkg/timezone"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
	"github.com/stretchr/testify/require"
)

// TestLegalTranslationKeepsConsent 覆盖译文核对、原文变更和协议确认版本。
func TestLegalTranslationKeepsConsent(t *testing.T) {
	zh := "zh-Hans"
	docs := []LoginAgreementDocument{{ID: "terms", Title: "条款", ContentMD: "正文"}}
	revision := BuildLoginAgreementRevision("2026-03-31", docs)
	raw, err := json.Marshal(docs)
	require.NoError(t, err)
	docs[0].Localization = &locale.Update[LoginAgreementCopy]{Content: locale.Original(LoginAgreementCopy{Title: "条款", ContentMD: "正文"}), ReviewedLocales: []string{"en"}}
	docs[0].Localization.SourceLocale = &zh
	docs[0].Localization.Translations["en"] = locale.Translation[LoginAgreementCopy]{Value: LoginAgreementCopy{Title: "Terms", ContentMD: "Content"}}
	saved, err := PrepareLegalDocuments(string(raw), docs)
	require.NoError(t, err)
	require.Equal(t, revision, BuildLoginAgreementRevision("2026-03-31", saved))
	copy, info := saved[0].Localization.Resolve("en")
	require.Equal(t, "Terms", copy.Title)
	require.False(t, info.Fallback)
	raw, err = json.Marshal(saved)
	require.NoError(t, err)
	saved[0].Localization.Source.ContentMD = "修改正文"
	changed, err := PrepareLegalDocuments(string(raw), saved)
	require.NoError(t, err)
	require.NotEqual(t, revision, BuildLoginAgreementRevision("2026-03-31", changed))
	copy, info = changed[0].Localization.Resolve("en")
	require.Equal(t, "修改正文", copy.ContentMD)
	require.True(t, info.Fallback)
}

// TestOptionalSiteTextDistinguishesEmpty 确认未配置与有意留空在公开展示中可以区分。
func TestOptionalSiteTextDistinguishesEmpty(t *testing.T) {
	values := map[string]string{}
	editor := ParseLocalizedTexts(values)
	saved, err := PrepareLocalizedTexts(values, editor)
	require.NoError(t, err)
	require.Empty(t, saved)
	en := "en"
	title := editor["site_title"]
	title.SourceLocale = &en
	saved, err = PrepareLocalizedTexts(values, LocalizedTexts{"site_title": title})
	require.NoError(t, err)
	require.Contains(t, saved, "site_title")
	require.Empty(t, saved["site_title"].Source)
	raw, err := json.Marshal(saved)
	require.NoError(t, err)
	values[SettingKeySiteTexts] = string(raw)
	require.Contains(t, ResolveSiteTexts(values, "zh-Hans"), "site_title")
}

// TestSiteTextOverrides 检查已保存的空文案与尚未配置的字段。
func TestSiteTextOverrides(t *testing.T) {
	en := "en"
	for _, tc := range []struct {
		name         string
		source       string
		language     *string
		revision     int64
		translations map[string]locale.Translation[string]
		configured   bool
	}{
		{name: "missing"},
		{name: "saved empty", revision: 1, configured: true},
		{name: "saved whitespace", source: " \t\n", revision: 1, configured: true},
		{name: "custom source", source: "自定义标题", revision: 1, configured: true},
		{name: "translated source", revision: 1, translations: map[string]locale.Translation[string]{"en": {Value: "Custom title", SourceRevision: 1}}, configured: true},
		{name: "intentional empty", language: &en, revision: 1, configured: true},
		{name: "edited content", revision: 2, configured: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := locale.Content[string]{SourceLocale: tc.language, Source: tc.source, Revision: tc.revision, SourceRevision: 1, Translations: tc.translations}
			for _, key := range []string{"site_title", "site_subtitle"} {
				contents := map[string]locale.Content[string]{}
				if tc.configured {
					contents[key] = doc
				}
				raw, err := json.Marshal(contents)
				require.NoError(t, err)
				values := map[string]string{SettingKeySiteTexts: string(raw)}
				_, configured := ConfiguredSiteTexts(values)[key]
				require.Equal(t, tc.configured, configured, key)
				editor := ParseLocalizedTexts(values)
				if !tc.configured {
					require.Zero(t, editor[key].Revision)
				}
				service := NewPublicService(homeTextSource(values), timezone.NewCalendar(time.UTC), "UTC")
				for _, language := range []string{"en", "zh-Hans"} {
					ctx := locale.WithLanguage(context.Background(), language)
					public, err := service.GetPublicSettings(ctx)
					require.NoError(t, err)
					if tc.configured {
						require.Contains(t, public.SiteTextOverrides, key)
					} else {
						require.NotContains(t, public.SiteTextOverrides, key)
					}
					injection, err := service.GetPublicSettingsForInjection(ctx)
					require.NoError(t, err)
					encoded, err := json.Marshal(injection)
					require.NoError(t, err)
					var injected struct {
						Overrides []string `json:"site_text_overrides"`
					}
					require.NoError(t, json.Unmarshal(encoded, &injected))
					require.Equal(t, public.SiteTextOverrides, injected.Overrides)
				}
			}
		})
	}
}

// homeTextSource 给公开接口与首屏注入提供相同的站点设置。
type homeTextSource map[string]string

func (s homeTextSource) LoadSitePublicInputs(context.Context) (PublicInputs, error) {
	return PublicInputs{Values: s}, nil
}

func (homeTextSource) PublicVersion() string { return "home-copy-test" }
