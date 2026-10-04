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

func TestFingerprintMatchesRequested(t *testing.T) {
	cases := []struct {
		requested  string
		prediction string
		want       bool
	}{
		{requested: "gpt-6-astra", prediction: "gpt-6-astra", want: true},
		{requested: "GPT-6-Astra", prediction: "gpt-6-astra", want: true},
		{requested: "gpt-6-astra-2026-10-01", prediction: "gpt-6-astra", want: true},
		{requested: "gpt-6-astra", prediction: "gpt-6-luna", want: false},
		{requested: "gpt-6-astra", prediction: "gpt-6-sol", want: false},
		{requested: "gpt-6-astra", prediction: "gpt-5.6-terra", want: false},
		{requested: "gpt-6-luna", prediction: "gpt-6-luna", want: true},
		{requested: "", prediction: "gpt-6-astra", want: false},
	}
	for _, tc := range cases {
		got := FingerprintMatchesRequested(tc.requested, tc.prediction)
		if got != tc.want {
			t.Fatalf("requested=%q prediction=%q got=%v want=%v", tc.requested, tc.prediction, got, tc.want)
		}
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
	if FingerprintMatchesRequested("gpt-6-astra", got.Prediction) {
		t.Fatalf("prediction = %s p=%f, want a model other than the requested gpt-6-astra", got.Prediction, got.Probability)
	}
	t.Logf("prediction = %s p=%.4f", got.Prediction, got.Probability)
}

func TestAnalyzeTraceOutputs_RejectsShortAnswer(t *testing.T) {
	_, err := AnalyzeTraceOutputs([]TraceOutput{{Text: "1 2 3", ExpectedCount: 331}})
	if err == nil {
		t.Fatal("short answer should be rejected")
	}
}
