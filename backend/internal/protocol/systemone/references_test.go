package systemone

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// references_test.go checks image reference presence through the public payload
// parser. Nested descriptions, overlapping prefixes, Unicode escapes, and
// separate JSON values exercise the matcher without depending on trie layout.
// A bounded-allocation regression and benchmarks protect large states with
// many assets from repeated JSON decoding and reference-by-reference rescans.

// TestImageReferenceMatching covers overlapping references and suffix recovery
// in nested descriptions. Object keys, separate string fragments, and numbers
// cannot establish a reference, while large JSON numbers preserve nearby text.
func TestImageReferenceMatching(t *testing.T) {
	for _, tc := range []struct {
		name       string   // fixture scenario name
		state      string   // raw state JSON
		references []string // requested image placeholders
		valid      bool     // expected complete reference coverage
	}{
		{"suffixes", `"ushers and his"`, []string{"he", "she", "hers", "his"}, true},
		{"repeated prefixes", `"aaaaaaab"`, []string{"a", "aa", "aaa", "aaaaab", "aab", "ab"}, true},
		{"recover suffix", `"ababc"`, []string{"ab", "bab", "abc", "bc"}, true},
		{"nested values", `{"values":["<one>",{"image":"before <two> after"}]}`, []string{"<one>", "<two>"}, true},
		{"unicode escape", `{"text":"\u56fe\u50cf and <\u00e9>"}`, []string{"图像", "<é>"}, true},
		{"large number", `{"number":1e400,"photo":"<one>"}`, []string{"<one>"}, true},
		{"key only", `{"<one>":"ordinary text"}`, []string{"<one>"}, false},
		{"split strings", `["<on","e>"]`, []string{"<one>"}, false},
		{"number only", `{"number":123}`, []string{"123"}, false},
		{"missing suffix", `"aaaaac"`, []string{"a", "aa", "aab"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assets := make(map[string]Asset, len(tc.references))
			for _, reference := range tc.references {
				assets[reference] = Asset{Format: "png", DataBase64: "YQ=="}
			}
			body, err := json.Marshal(Request{Model: "jev-latest", State: json.RawMessage(tc.state), Questions: map[string]Question{"box": {Type: "box"}}, Assets: assets})
			require.NoError(t, err)
			_, err = ParseRequest(body)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "must appear")
			}
		})
	}
}

// TestImageReferencesMatchSubstringSemantics compares many deterministic input
// combinations with ordinary string-value substring checks. Shared prefixes,
// repeated matches, and missing references must preserve parser acceptance.
func TestImageReferencesMatchSubstringSemantics(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	for iteration := range 200 {
		assets := map[string]Asset{}
		for range 20 {
			assets[referenceSample(random, 1+random.IntN(8))] = Asset{Format: "png", DataBase64: "YQ=="}
		}
		values := []string{referenceSample(random, 80), referenceSample(random, 80)}
		if iteration%2 == 0 {
			for reference := range assets {
				values = append(values, reference)
			}
		}
		valid := true
		for reference := range assets {
			found := false
			for _, value := range values {
				found = found || strings.Contains(value, reference)
			}
			valid = valid && found
		}
		state, err := json.Marshal(map[string]any{"nested": values})
		require.NoError(t, err)
		body, err := json.Marshal(Request{Model: "jev-latest", State: state, Questions: map[string]Question{"box": {Type: "box"}}, Assets: assets})
		require.NoError(t, err)
		_, err = ParseRequest(body)
		require.Equal(t, valid, err == nil, "iteration %d: %v", iteration, err)
	}
}

// referenceSample creates deterministic mixed UTF-8 text for parser fixtures.
func referenceSample(random *rand.Rand, length int) string {
	alphabet := []rune("abc<图é>")
	value := make([]rune, length)
	for index := range value {
		value[index] = alphabet[random.IntN(len(alphabet))]
	}
	return string(value)
}

// TestImageReferenceMemoryIsBounded parses a two-MiB state with five hundred
// assets. Allocated bytes must remain below a generous fixed regression budget,
// independent of elapsed time or machine speed; repeated decoding exceeds it.
func TestImageReferenceMemoryIsBounded(t *testing.T) {
	body := largeReferencePayload(t, 500)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := ParseRequest(body)
	runtime.ReadMemStats(&after)
	require.NoError(t, err)
	// Sixty-four MiB leaves room for parser and matcher allocations across Go
	// releases, but excludes the roughly two-GiB repeated-decoding regression.
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(64<<20))
}

// BenchmarkImageReferences varies reference count beside an unchanged large
// state. Reported bytes and allocations make repeated decoding or scanning
// visible without introducing wall-clock thresholds into ordinary unit tests.
func BenchmarkImageReferences(b *testing.B) {
	for _, count := range []int{1, 100, 500} {
		b.Run(fmt.Sprintf("assets_%d", count), func(b *testing.B) {
			body := largeReferencePayload(b, count)
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseRequest(body); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// largeReferencePayload builds a bounded large-state fixture with every image referenced.
// Image bytes remain opaque to the local protocol validator; upstream image
// decoding is outside this allocation and substring-matching regression fixture.
func largeReferencePayload(t testing.TB, count int) []byte {
	t.Helper()
	assets := make(map[string]Asset, count)
	var state strings.Builder
	_, err := state.WriteString(strings.Repeat("x", 2<<20))
	require.NoError(t, err)
	for index := range count {
		reference := fmt.Sprintf("<image-%04d>", index)
		_, err = state.WriteString(reference)
		require.NoError(t, err)
		assets[reference] = Asset{Format: "png", DataBase64: "YQ=="}
	}
	encodedState, err := json.Marshal(state.String())
	require.NoError(t, err)
	body, err := json.Marshal(Request{Model: "jev-latest", State: encodedState, Questions: map[string]Question{"box": {Type: "box"}}, Assets: assets})
	require.NoError(t, err)
	return body
}
