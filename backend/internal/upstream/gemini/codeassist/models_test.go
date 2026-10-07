package codeassist

import "testing"

func TestDefaultModels_ContainsImageModels(t *testing.T) {
	t.Parallel()

	byID := make(map[string]Model, len(DefaultModels))
	for _, model := range DefaultModels {
		byID[model.ID] = model
	}

	required := []string{
		"gemini-2.5-flash-image",
		"gemini-3.1-flash-image",
	}

	for _, id := range required {
		if _, ok := byID[id]; !ok {
			t.Fatalf("expected curated Gemini model %q to exist", id)
		}
	}
}

func TestGoogleOneModels_ExcludeUnsupportedNewAndImageModels(t *testing.T) {
	t.Parallel()

	models := make(map[string]struct{}, len(GoogleOneModels))
	for _, model := range GoogleOneModels {
		models[model.ID] = struct{}{}
	}
	for _, id := range []string{"gemini-2.0-flash", "gemini-2.5-flash", "gemini-2.5-pro"} {
		if _, ok := models[id]; !ok {
			t.Fatalf("Google One 目录缺少型号 %q", id)
		}
	}
	for _, id := range []string{"gemini-2.5-flash-image", "gemini-3.1-flash-image", "gemini-3.5-flash"} {
		if _, ok := models[id]; ok {
			t.Fatalf("did not expect unsupported Google One model %q", id)
		}
	}
}
