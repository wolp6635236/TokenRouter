//go:build unit

package qualityprobe

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseFingerprintNumbers_LongestRun(t *testing.T) {
	got := parseFingerprintNumbers("note 12 then 1 2 3 extra")
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Fatalf("got = %v", got)
	}
}

func TestFingerprintMatchesGPT6(t *testing.T) {
	if !FingerprintMatchesGPT6("gpt-6-astra") || !FingerprintMatchesGPT6("gpt-6-sol") {
		t.Fatal("gpt-6 should pass")
	}
	if FingerprintMatchesGPT6("gpt-5.6-terra") || FingerprintMatchesGPT6("claude-opus-5") {
		t.Fatal("non gpt-6 should fail")
	}
}

func TestAnalyzeTraceOutputs_TerraSequence(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "testdata", "terra_numbers.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := AnalyzeTraceOutputs([]TraceOutput{{
		Text:          string(raw),
		ExpectedCount: 331,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if FingerprintMatchesGPT6(got.Prediction) {
		t.Fatalf("prediction = %s p=%f, want a non-gpt-6 model", got.Prediction, got.Probability)
	}
	t.Logf("prediction = %s p=%.4f", got.Prediction, got.Probability)
}

func TestAnalyzeTraceOutputs_RejectsShortAnswer(t *testing.T) {
	_, err := AnalyzeTraceOutputs([]TraceOutput{{Text: "1 2 3", ExpectedCount: 331}})
	if err == nil {
		t.Fatal("short answer should be rejected")
	}
}
