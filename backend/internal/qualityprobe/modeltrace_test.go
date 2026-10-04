//go:build unit

package qualityprobe

import (
	"math/rand"
	"strings"
	"testing"
)

func TestBuildTraceChallenges_HasNumericTask(t *testing.T) {
	challenges := BuildTraceChallenges(rand.New(rand.NewSource(1)))
	if len(challenges) != 1 {
		t.Fatalf("len = %d, want 1", len(challenges))
	}
	for _, item := range challenges {
		if item.ExpectedCount < 292 || item.ExpectedCount > 332 {
			t.Fatalf("count %d out of range", item.ExpectedCount)
		}
		if !strings.Contains(item.Prompt, "1 到 355") {
			t.Fatalf("prompt missing range: %s", item.Prompt)
		}
	}
}

func TestTraceSampleValid_RequiresFiftyFivePercentNumbers(t *testing.T) {
	expected := 100
	if TraceSampleValid("1 2 3", expected) {
		t.Fatal("too few numbers must fail")
	}
	parts := make([]string, 80)
	for i := range parts {
		parts[i] = "1"
	}
	if !TraceSampleValid(strings.Join(parts, " "), expected) {
		t.Fatal("80 numbers should pass the minimum-count threshold")
	}
}
