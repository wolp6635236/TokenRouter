package upstream

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSanitizedUpstreamPathSuffixRejectsNonConformingSegments 验证上游路径后缀拒绝不合规的路径段。
func TestSanitizedUpstreamPathSuffixRejectsNonConformingSegments(t *testing.T) {
	// 到达业务代码的 URL.Path 已是百分号解码后的结果，因此用例按解码后的形态书写。
	rejected := []string{
		"/..",
		"/../..",
		"/../../x/y",
		"/./compact",
		"/compact/..",
		`/..\..\x`,
		`/compact\..`,
		"/?a=b",
		"/compact?a=b",
		"/compact#frag",
		"/compact%2f..",
		"/100%",
		"//double",
		"/compact//detail",
		"/compact/",
		"/ compact",
		"/compact\x00",
		"/compact\nX-Injected: 1",
		"/模型",
		"compact",
		"/a:b",
		"/a;b",
		"/a,b",
		"/a=b",
		"/a&b",
		// 路径段字符允许 `\w`、`-` 和 `.`，其余字符拒绝。
		"/a~b",
		"/a@b",
		"/a+b",
		"/a|b",
		"/a*b",
		"/a$b",
		"/a(b)",
		"/a'b",
		"/a\"b",
		"/a<b",
		"/a\tb",
		"/a b",
		"/a∕b", // DIVISION SLASH
		"/a／b", // FULLWIDTH SOLIDUS
		// 只由点组成的片段一律拒绝（各实现对其解释不一致）。
		"/...",
		"/....",
		"/compact/...",
	}
	for _, suffix := range rejected {
		t.Run("reject_"+suffix, func(t *testing.T) {
			got, ok := SanitizedUpstreamPathSuffix(suffix)
			require.False(t, ok, "suffix %q must be rejected", suffix)
			require.Empty(t, got)
		})
	}

	accepted := map[string]string{
		"":                          "",
		"/compact":                  "/compact",
		"/compact/detail":           "/compact/detail",
		"/resp_68f0a1b2c3d4/cancel": "/resp_68f0a1b2c3d4/cancel",
		"/gemini-2.5-pro_v1.2":      "/gemini-2.5-pro_v1.2",
		"/a.b.c":                    "/a.b.c",
	}
	for suffix, want := range accepted {
		t.Run("accept_"+suffix, func(t *testing.T) {
			got, ok := SanitizedUpstreamPathSuffix(suffix)
			require.True(t, ok, "suffix %q must be accepted", suffix)
			require.Equal(t, want, got)
		})
	}
}

func TestSanitizedUpstreamPathSuffixEnforcesBounds(t *testing.T) {
	longSegment := "/"
	for range MaxUpstreamPathSegmentLen + 1 {
		longSegment += "a"
	}
	_, ok := SanitizedUpstreamPathSuffix(longSegment)
	require.False(t, ok, "over-long segment must be rejected")

	deep := ""
	for i := 0; i <= MaxUpstreamPathSegments; i++ {
		deep += "/a"
	}
	_, ok = SanitizedUpstreamPathSuffix(deep)
	require.False(t, ok, "over-deep suffix must be rejected")
}
