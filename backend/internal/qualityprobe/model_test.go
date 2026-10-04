//go:build unit

package qualityprobe

import (
	"testing"
	"time"
)

func TestResolveProbeModel(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		catalog    []string
		want       string
	}{
		{
			name:       "默认 Astra",
			configured: "",
			catalog:    nil,
			want:       DefaultProbeModel,
		},
		{
			name:       "管理员指定模型",
			configured: "gpt-5.6-sol",
			catalog:    []string{"gpt-6-astra", "gpt-5.6-sol"},
			want:       "gpt-5.6-sol",
		},
		{
			name:       "latest 从目录取最新 Astra",
			configured: "latest",
			catalog:    []string{"gpt-5.6-sol", "gpt-6-astra", "gpt-6-astra-2026-10-01"},
			want:       "gpt-6-astra-2026-10-01",
		},
		{
			name:       "latest 目录没有 Astra 时回退默认",
			configured: "latest",
			catalog:    []string{"gpt-5.6-sol"},
			want:       DefaultProbeModel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveProbeModel(tt.configured, tt.catalog)
			if got != tt.want {
				t.Fatalf("ResolveProbeModel(%q) = %q, want %q", tt.configured, got, tt.want)
			}
		})
	}
}

func TestDefaultSettings(t *testing.T) {
	got := DefaultSettings()
	if got.Enabled {
		t.Fatal("feature starts disabled")
	}
	if got.Interval != 30*time.Minute {
		t.Fatalf("interval = %s, want 30m", got.Interval)
	}
	if got.Model != DefaultProbeModel {
		t.Fatalf("model = %q, want %q", got.Model, DefaultProbeModel)
	}
	if got.Cooldown != 5*time.Minute {
		t.Fatalf("cooldown = %s, want 5m", got.Cooldown)
	}
	if got.MaxAttempts != 3 {
		t.Fatalf("max attempts = %d, want 3", got.MaxAttempts)
	}
	if got.NotifyEmail != DefaultNotifyEmail {
		t.Fatalf("email = %q, want %q", got.NotifyEmail, DefaultNotifyEmail)
	}
}

func TestCatalogContainsModel(t *testing.T) {
	if !CatalogContainsModel([]string{"gpt-5.6-terra", "gpt-6-astra"}, "gpt-6-astra") {
		t.Fatal("exact astra should match")
	}
	if !CatalogContainsModel([]string{"gpt-6-astra-2026-10-01"}, "gpt-6-astra") {
		t.Fatal("dated astra should match settings astra")
	}
	if CatalogContainsModel([]string{"gpt-5.6-terra", "gpt-6-sol"}, "gpt-6-astra") {
		t.Fatal("terra whitelist should not match astra")
	}
	if !CatalogContainsModel([]string{"gpt-6-*"}, "gpt-6-astra") {
		t.Fatal("wildcard should match")
	}
}
