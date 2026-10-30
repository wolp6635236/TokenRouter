package postgres

import (
	"strings"
	"testing"
)

// TestTruncateText covers byte budgets, whitespace, and malformed input around
// diagnostic-field limits. Fitting input retains its bytes, while oversized
// input follows the existing decoded-rune truncation contract.
func TestTruncateText(t *testing.T) {
	tests := []struct {
		name  string // boundary scenario name
		input string // supplied diagnostic text
		limit int    // maximum output bytes
		want  string // expected retained text
	}{
		{"trim", "  abc\n", 3, "abc"},
		{"ascii", "abcdef", 3, "abc"},
		{"multibyte", "你好x", 4, "你"},
		{"exact_multibyte", "你好", 6, "你好"},
		{"insufficient_rune", "你", 2, ""},
		{"zero", "abc", 0, ""},
		{"negative", "abc", -1, ""},
		{"empty_negative", "", -1, ""},
		{"fitting_invalid", "\xffx", 2, "\xffx"},
		{"truncated_invalid", "a\xffbc", 3, "a"},
		{"normalized_invalid", "a\xffbcd", 4, "a\ufffd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateText(tt.input, tt.limit); got != tt.want {
				t.Fatalf("TruncateText(%q, %d) = %q, want %q", tt.input, tt.limit, got, tt.want)
			}
		})
	}
}

// BenchmarkTruncateText measures truncation of oversized multibyte diagnostic
// metadata to a typical audit-field budget. A fixed input keeps repeated
// benchmark runs comparable when the truncation algorithm changes.
func BenchmarkTruncateText(b *testing.B) {
	input := strings.Repeat("诊断", 2048)
	b.ReportAllocs()
	for b.Loop() {
		TruncateText(input, 512)
	}
}
