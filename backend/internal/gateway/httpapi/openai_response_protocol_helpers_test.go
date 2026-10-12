package httpapi

import (
	"strings"
	"testing"
)

// TestSummarizeNoOutputBody_RespectsLogBodyConfig 检查错误体日志关闭时的结构化诊断。
// 原始上游 body 片段由 gateway.log_upstream_error_body 控制。
func TestSummarizeNoOutputBody_RespectsLogBodyConfig(t *testing.T) {
	body := []byte("data: {\"type\":\"response.in_progress\",\"response\":{\"status\":\"in_progress\"}}\n\n")
	svc := newImagesFixture(imagesFixtureInputs{})

	summary := svc.Output.ImageNoOutputSummary(body)
	if !strings.Contains(summary, "last_event=response.in_progress") {
		t.Fatalf("summary should keep structured diagnostics, got %q", summary)
	}
	if strings.Contains(summary, " body=") {
		t.Fatalf("summary should omit body when log_upstream_error_body is disabled, got %q", summary)
	}
}
