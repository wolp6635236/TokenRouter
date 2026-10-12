package openai

import (
	"testing"
)

func TestNormalizedCodexLimits(t *testing.T) {
	// 直接检查 Normalize 返回的配额窗口。
	pUsed := 100.0
	pReset := 384607
	pWindow := 10080
	sUsed := 3.0
	sReset := 17369
	sWindow := 300

	snapshot := &OpenAICodexUsageSnapshot{
		PrimaryUsedPercent:         &pUsed,
		PrimaryResetAfterSeconds:   &pReset,
		PrimaryWindowMinutes:       &pWindow,
		SecondaryUsedPercent:       &sUsed,
		SecondaryResetAfterSeconds: &sReset,
		SecondaryWindowMinutes:     &sWindow,
	}

	normalized := snapshot.Normalize()
	if normalized == nil {
		t.Fatal("expected non-nil normalized")
	}

	// primary 的窗口为 10080 分钟，归入七天额度。
	if normalized.Used7dPercent == nil || *normalized.Used7dPercent != 100.0 {
		t.Errorf("expected Used7dPercent=100, got %v", normalized.Used7dPercent)
	}
	if normalized.Reset7dSeconds == nil || *normalized.Reset7dSeconds != 384607 {
		t.Errorf("expected Reset7dSeconds=384607, got %v", normalized.Reset7dSeconds)
	}
	if normalized.Used5hPercent == nil || *normalized.Used5hPercent != 3.0 {
		t.Errorf("expected Used5hPercent=3, got %v", normalized.Used5hPercent)
	}
	if normalized.Reset5hSeconds == nil || *normalized.Reset5hSeconds != 17369 {
		t.Errorf("expected Reset5hSeconds=17369, got %v", normalized.Reset5hSeconds)
	}
}

func TestNormalizedCodexLimits_OnlyPrimaryData(t *testing.T) {
	// 仅 primary 有数据，窗口时长缺失。
	pUsed := 80.0
	pReset := 50000

	snapshot := &OpenAICodexUsageSnapshot{
		PrimaryUsedPercent:       &pUsed,
		PrimaryResetAfterSeconds: &pReset,
		// 未提供窗口时长和 secondary 数据。
	}

	normalized := snapshot.Normalize()
	if normalized == nil {
		t.Fatal("expected non-nil normalized")
	}

	// 缺少窗口时长时，primary 归入七天额度，secondary 归入五小时额度。
	if normalized.Used7dPercent == nil || *normalized.Used7dPercent != 80.0 {
		t.Errorf("expected Used7dPercent=80, got %v", normalized.Used7dPercent)
	}
	if normalized.Reset7dSeconds == nil || *normalized.Reset7dSeconds != 50000 {
		t.Errorf("expected Reset7dSeconds=50000, got %v", normalized.Reset7dSeconds)
	}
	// 五小时额度为空。
	if normalized.Used5hPercent != nil {
		t.Errorf("expected Used5hPercent=nil, got %v", *normalized.Used5hPercent)
	}
	if normalized.Reset5hSeconds != nil {
		t.Errorf("expected Reset5hSeconds=nil, got %v", *normalized.Reset5hSeconds)
	}
}

func TestNormalizedCodexLimits_OnlySecondaryData(t *testing.T) {
	// 仅 secondary 有数据，窗口时长缺失。
	sUsed := 60.0
	sReset := 3000

	snapshot := &OpenAICodexUsageSnapshot{
		SecondaryUsedPercent:       &sUsed,
		SecondaryResetAfterSeconds: &sReset,
		// 未提供窗口时长和 primary 数据。
	}

	normalized := snapshot.Normalize()
	if normalized == nil {
		t.Fatal("expected non-nil normalized")
	}

	// 缺少窗口时长时，primary 归入七天额度，secondary 归入五小时额度。
	// secondary 归入五小时额度。
	if normalized.Used5hPercent == nil || *normalized.Used5hPercent != 60.0 {
		t.Errorf("expected Used5hPercent=60, got %v", normalized.Used5hPercent)
	}
	if normalized.Reset5hSeconds == nil || *normalized.Reset5hSeconds != 3000 {
		t.Errorf("expected Reset5hSeconds=3000, got %v", normalized.Reset5hSeconds)
	}
	// 七天额度为空。
	if normalized.Used7dPercent != nil {
		t.Errorf("expected Used7dPercent=nil, got %v", *normalized.Used7dPercent)
	}
}

func TestNormalizedCodexLimits_BothDataNoWindowMinutes(t *testing.T) {
	// 两个窗口均有数据，窗口时长缺失。
	pUsed := 100.0
	pReset := 400000
	sUsed := 50.0
	sReset := 10000

	snapshot := &OpenAICodexUsageSnapshot{
		PrimaryUsedPercent:         &pUsed,
		PrimaryResetAfterSeconds:   &pReset,
		SecondaryUsedPercent:       &sUsed,
		SecondaryResetAfterSeconds: &sReset,
		// 未提供窗口时长。
	}

	normalized := snapshot.Normalize()
	if normalized == nil {
		t.Fatal("expected non-nil normalized")
	}

	// 缺少窗口时长时，primary 归入七天额度，secondary 归入五小时额度。
	if normalized.Used7dPercent == nil || *normalized.Used7dPercent != 100.0 {
		t.Errorf("expected Used7dPercent=100, got %v", normalized.Used7dPercent)
	}
	if normalized.Reset7dSeconds == nil || *normalized.Reset7dSeconds != 400000 {
		t.Errorf("expected Reset7dSeconds=400000, got %v", normalized.Reset7dSeconds)
	}
	if normalized.Used5hPercent == nil || *normalized.Used5hPercent != 50.0 {
		t.Errorf("expected Used5hPercent=50, got %v", normalized.Used5hPercent)
	}
	if normalized.Reset5hSeconds == nil || *normalized.Reset5hSeconds != 10000 {
		t.Errorf("expected Reset5hSeconds=10000, got %v", normalized.Reset5hSeconds)
	}
}
