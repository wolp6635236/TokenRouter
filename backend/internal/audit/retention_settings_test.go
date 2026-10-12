package audit

import (
	"testing"
)

func TestParseAuditLogRetentionDays(t *testing.T) {
	cases := map[string]int{
		"":       DefaultRetentionDays,
		"abc":    DefaultRetentionDays,
		"90":     90,
		"0":      0,
		"-1":     0,
		"  30  ": 30,
	}
	for in, want := range cases {
		if got := ParseRetentionDays(in); got != want {
			t.Fatalf("parseAuditLogRetentionDays(%q) = %d, want %d", in, got, want)
		}
	}
}
