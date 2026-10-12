package clientmeta

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
)

const claudeCodeMetadataUserIDJSON = `{"device_id":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","account_uuid":"","session_id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}`

func TestSystemPromptSimilarity(t *testing.T) {
	v := NewClaudeCodeValidator()

	tests := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"精确匹配", "You are Claude Code, Anthropic's official CLI for Claude.", true},
		{"带多余空格", "You  are  Claude  Code,  Anthropic's  official  CLI  for  Claude.", true},
		{"Agent SDK 模板", "You are a Claude agent, built on Anthropic's Claude Agent SDK.", true},
		{"文件搜索专家模板", "You are a file search specialist for Claude Code, Anthropic's official CLI for Claude.", true},
		{"对话摘要模板", "You are a helpful AI assistant tasked with summarizing conversations.", true},
		{"交互式 CLI 模板", "You are an interactive CLI tool that helps users", true},
		{"无关文本", "Write me a poem about cats", false},
		{"空文本", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{
				"model": "claude-sonnet-4",
				"system": []any{
					map[string]any{"type": "text", "text": tt.prompt},
				},
			}
			result := v.hasClaudeCodeSystemPrompt(body)
			require.Equal(t, tt.want, result, "提示词: %q", tt.prompt)
		})
	}
}

func TestDiceCoefficient(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want float64
		tol  float64
	}{
		{"相同字符串", "hello", "hello", 1.0, 0.001},
		{"完全不同", "abc", "xyz", 0.0, 0.001},
		{"空字符串", "", "hello", 0.0, 0.001},
		{"单字符", "a", "b", 0.0, 0.001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DiceCoefficient(tt.a, tt.b)
			require.InDelta(t, tt.want, result, tt.tol)
		})
	}
}

// TestClaudeCodeValidationInputPreservesGateOrder 检查识别按 UA、探测绕过、报文校验的顺序执行。
func TestClaudeCodeValidationInputPreservesGateOrder(t *testing.T) {
	validator := NewClaudeCodeValidator()
	cases := []struct {
		name  string
		input ClaudeCodeValidationInput
		want  bool
	}{
		{"invalid UA with probe", ClaudeCodeValidationInput{Path: "/v1/messages", UserAgent: "curl/1.0.0", MaxTokensOneHaiku: true}, false},
		{"CLI probe", ClaudeCodeValidationInput{Path: "/v1/messages", UserAgent: "claude-cli/2.1.156", MaxTokensOneHaiku: true}, true},
		{"messages requires body", ClaudeCodeValidationInput{Path: "/v1/messages", UserAgent: "claude-cli/2.1.156"}, false},
		{"count tokens bypass", ClaudeCodeValidationInput{Path: "/v1/messages/count_tokens", UserAgent: "claude-cli/2.1.156"}, true},
		{"models bypass", ClaudeCodeValidationInput{Path: "/v1/models", UserAgent: "claude-cli/2.1.156"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, validator.Validate(tc.input, nil)) })
	}
}

func newTestValidator() *ClaudeCodeValidator {
	return NewClaudeCodeValidator()
}

// validClaudeCodeBody 构造一个完整有效的 Claude Code 请求体。
func validClaudeCodeBody() map[string]any {
	return map[string]any{
		"model": "claude-sonnet-4-20250514",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "You are Claude Code, Anthropic's official CLI for Claude.",
			},
		},
		"metadata": map[string]any{
			"user_id": "user_" + "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" + "_account__session_" + "12345678-1234-1234-1234-123456789abc",
		},
	}
}

func TestValidate_ClaudeCLIUserAgent(t *testing.T) {
	v := newTestValidator()

	tests := []struct {
		name string
		ua   string
		want bool
	}{
		{"标准版本号", "claude-cli/1.0.0", true},
		{"多位版本号", "claude-cli/12.34.56", true},
		{"大写开头", "Claude-CLI/1.0.0", true},
		{"非 claude-cli", "curl/7.64.1", false},
		{"空 User-Agent", "", false},
		{"部分匹配", "not-claude-cli/1.0.0", false},
		{"缺少版本号", "claude-cli/", false},
		{"版本格式不对", "claude-cli/1.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, v.ValidateUserAgent(tt.ua), "UA: %q", tt.ua)
		})
	}
}

func TestValidate_NonMessagesPath_UAOnly(t *testing.T) {
	v := newTestValidator()

	// 非 messages 路径只检查 UA
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("User-Agent", "claude-cli/1.0.0")

	result := v.Validate(claudeCodeInputFixture(req), nil)
	require.True(t, result, "非 messages 路径只需 UA 匹配")
}

func TestValidate_NonMessagesPath_InvalidUA(t *testing.T) {
	v := newTestValidator()

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("User-Agent", "curl/7.64.1")

	result := v.Validate(claudeCodeInputFixture(req), nil)
	require.False(t, result, "UA 不匹配时应返回 false")
}

func TestValidate_MessagesPath_FullValid(t *testing.T) {
	v := newTestValidator()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/1.0.0")
	req.Header.Set("X-App", "claude-code")
	req.Header.Set("anthropic-beta", "max-tokens-3-5-sonnet-2024-07-15")
	req.Header.Set("anthropic-version", "2023-06-01")

	result := v.Validate(claudeCodeInputFixture(req), validClaudeCodeBody())
	require.True(t, result, "完整有效请求应通过")
}

func TestValidate_MessagesPath_MissingHeaders(t *testing.T) {
	v := newTestValidator()
	body := validClaudeCodeBody()

	tests := []struct {
		name          string
		missingHeader string
	}{
		{"缺少 X-App", "X-App"},
		{"缺少 anthropic-beta", "anthropic-beta"},
		{"缺少 anthropic-version", "anthropic-version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			req.Header.Set("User-Agent", "claude-cli/1.0.0")
			req.Header.Set("X-App", "claude-code")
			req.Header.Set("anthropic-beta", "beta")
			req.Header.Set("anthropic-version", "2023-06-01")
			req.Header.Del(tt.missingHeader)

			result := v.Validate(claudeCodeInputFixture(req), body)
			require.False(t, result, "缺少 %s 应返回 false", tt.missingHeader)
		})
	}
}

func TestValidate_MessagesPath_InvalidMetadataUserID(t *testing.T) {
	v := newTestValidator()

	tests := []struct {
		name     string
		metadata map[string]any
	}{
		{"缺少 metadata", nil},
		{"缺少 user_id", map[string]any{"other": "value"}},
		{"空 user_id", map[string]any{"user_id": ""}},
		{"格式错误", map[string]any{"user_id": "invalid-format"}},
		{"hex 长度不足", map[string]any{"user_id": "user_abc_account__session_uuid"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			req.Header.Set("User-Agent", "claude-cli/1.0.0")
			req.Header.Set("X-App", "claude-code")
			req.Header.Set("anthropic-beta", "beta")
			req.Header.Set("anthropic-version", "2023-06-01")

			body := map[string]any{
				"model": "claude-sonnet-4",
				"system": []any{
					map[string]any{
						"type": "text",
						"text": "You are Claude Code, Anthropic's official CLI for Claude.",
					},
				},
			}
			if tt.metadata != nil {
				body["metadata"] = tt.metadata
			}

			result := v.Validate(claudeCodeInputFixture(req), body)
			require.False(t, result, "metadata.user_id: %v", tt.metadata)
		})
	}
}

func TestValidate_MessagesPath_InvalidSystemPrompt(t *testing.T) {
	v := newTestValidator()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/1.0.0")
	req.Header.Set("X-App", "claude-code")
	req.Header.Set("anthropic-beta", "beta")
	req.Header.Set("anthropic-version", "2023-06-01")

	body := map[string]any{
		"model": "claude-sonnet-4",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "Generate JSON data for testing database migrations.",
			},
		},
		"metadata": map[string]any{
			"user_id": "user_" + "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2" + "_account__session_12345678-1234-1234-1234-123456789abc",
		},
	}

	result := v.Validate(claudeCodeInputFixture(req), body)
	require.False(t, result, "无关系统提示词应返回 false")
}

func TestValidate_MaxTokensOneHaikuBypass(t *testing.T) {
	v := newTestValidator()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/1.0.0")
	// 不设置 X-App 等头，通过 context 标记为 haiku 探测请求
	ctx := requeststate.WithIsMaxTokensOneHaikuRequest(req.Context(), true)
	req = req.WithContext(ctx)

	// 即使 body 不包含 system prompt，也应通过
	result := v.Validate(claudeCodeInputFixture(req), map[string]any{"model": "claude-3-haiku", "max_tokens": 1})
	require.True(t, result, "max_tokens=1+haiku 探测请求应绕过严格验证")
}

func TestValidate_NilBody_MessagesPath(t *testing.T) {
	v := newTestValidator()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/1.0.0")
	req.Header.Set("X-App", "claude-code")
	req.Header.Set("anthropic-beta", "beta")
	req.Header.Set("anthropic-version", "2023-06-01")

	result := v.Validate(claudeCodeInputFixture(req), nil)
	require.False(t, result, "nil body 的 messages 请求应返回 false")
}

func TestIsClaudeCodeClient(t *testing.T) {
	// 合法的 legacy 格式 metadata.user_id（64 位 hex + provider uuid + session uuid）
	legacyUserID := "user_a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2_account_550e8400-e29b-41d4-a716-446655440000_session_123e4567-e89b-12d3-a456-426614174000"
	// 合法的 JSON 格式 metadata.user_id（2.1.78+ 版本）
	jsonUserID := `{"device_id":"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2","account_uuid":"550e8400-e29b-41d4-a716-446655440000","session_id":"123e4567-e89b-12d3-a456-426614174000"}`

	tests := []struct {
		name           string
		userAgent      string
		metadataUserID string
		want           bool
	}{
		{
			name:           "Claude Code client with legacy user_id",
			userAgent:      "claude-cli/1.0.62 (darwin; arm64)",
			metadataUserID: legacyUserID,
			want:           true,
		},
		{
			name:           "Claude Code client with JSON user_id",
			userAgent:      "claude-cli/2.1.92 (external, cli)",
			metadataUserID: jsonUserID,
			want:           true,
		},
		{
			name:           "Claude Code case insensitive UA",
			userAgent:      "Claude-CLI/2.0.0",
			metadataUserID: legacyUserID,
			want:           true,
		},
		{
			name:           "Missing metadata user_id",
			userAgent:      "claude-cli/1.0.0",
			metadataUserID: "",
			want:           false,
		},
		{
			name:           "Claude CLI UA with invalid user_id format",
			userAgent:      "claude-cli/2.0.0",
			metadataUserID: "fake-user-id-12345",
			want:           false,
		},
		{
			name:           "Different user agent with valid user_id",
			userAgent:      "curl/7.68.0",
			metadataUserID: legacyUserID,
			want:           false,
		},
		{
			name:           "Empty user agent",
			userAgent:      "",
			metadataUserID: legacyUserID,
			want:           false,
		},
		{
			name:           "Similar but not Claude CLI",
			userAgent:      "claude-api/1.0.0",
			metadataUserID: legacyUserID,
			want:           false,
		},
		{
			name:           "Opencode spoofing UA with arbitrary user_id",
			userAgent:      "claude-cli/2.1.92",
			metadataUserID: "session_abc",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsClaudeCodeClient(tt.userAgent, tt.metadataUserID)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestClaudeCodeValidator_ProbeBypass(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/1.2.3 (darwin; arm64)")
	req = req.WithContext(requeststate.WithIsMaxTokensOneHaikuRequest(req.Context(), true))

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model":      "claude-haiku-4-5",
		"max_tokens": 1,
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_ProbeBypassRequiresUA(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "curl/8.0.0")
	req = req.WithContext(requeststate.WithIsMaxTokensOneHaikuRequest(req.Context(), true))

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model":      "claude-haiku-4-5",
		"max_tokens": 1,
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_MessagesWithoutProbeStillNeedStrictValidation(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/1.2.3 (darwin; arm64)")

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model":      "claude-haiku-4-5",
		"max_tokens": 1,
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_CountTokensPathUAOnly(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages/count_tokens", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.156 (Claude Code)")

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model": "claude-opus-4-8",
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_CountTokensPathRequiresUA(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages/count_tokens", nil)
	req.Header.Set("User-Agent", "curl/8.0.0")

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model": "claude-opus-4-8",
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_MessagesPathFullValid(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.156 (Claude Code)")
	req.Header.Set("X-App", "claude-code")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model":  "claude-opus-4-8",
		"stream": true,
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "You are Claude Code, Anthropic's official CLI for Claude.",
			},
		},
		"metadata": map[string]any{
			"user_id": "user_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_account__session_aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		},
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_BillingBlockRecognizedWithoutIdentityPrompt(t *testing.T) {
	// 使用抓取的安全监视器系统提示词，内容没有身份说明文本。
	monitorPrompt, err := os.ReadFile("testdata/security_monitor_system_prompt.txt")
	require.NoError(t, err)

	validator := NewClaudeCodeValidator()

	// 监视器正文与身份说明文本的 Dice 相似度低于阈值，此例由计费归因块识别放行。
	require.Less(t, validator.BestSimilarityScore(string(monitorPrompt)), ClaudeCodeSystemPromptThreshold)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.162 (external, cli)")
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	// Claude Code 安全监视器子请求通过 system 数组中的计费归因块及 cc_entrypoint=cli 识别。
	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model": "claude-3-5-haiku-20241022",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "x-anthropic-billing-header: cc_version=2.1.162.884; cc_entrypoint=cli; cch=d8726;",
			},
			map[string]any{
				"type": "text",
				"text": string(monitorPrompt),
			},
		},
		"metadata": map[string]any{
			"user_id": claudeCodeMetadataUserIDJSON,
		},
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_SecurityMonitorWithoutBillingBlock(t *testing.T) {
	monitorPrompt, err := os.ReadFile("testdata/security_monitor_system_prompt.txt")
	require.NoError(t, err)

	validHeaders := map[string]string{
		"User-Agent":        "claude-cli/2.1.220 (external, cli)",
		"X-App":             "cli",
		"anthropic-beta":    "claude-code-20250219",
		"anthropic-version": "2023-06-01",
	}
	validBody := func(prompt string) map[string]any {
		return map[string]any{
			"model": "claude-haiku-4-5-20251001",
			"system": []any{
				map[string]any{"type": "text", "text": prompt},
			},
			"metadata": map[string]any{"user_id": claudeCodeMetadataUserIDJSON},
		}
	}
	// CLI 会在安全监视器提示词之外追加随会话变化的上下文块。
	sessionContext := "\n\n## Session Context\n\n- **User identity**: testuser\n" +
		"- **Working directory**: /home/testuser/project\n- **Platform**: linux"

	tests := []struct {
		name       string
		headers    map[string]string
		body       map[string]any
		wantAccept bool
	}{
		{
			name:       "official classifier request",
			headers:    validHeaders,
			body:       validBody(string(monitorPrompt)),
			wantAccept: true,
		},
		{
			name: "non-Claude user agent",
			headers: map[string]string{
				"User-Agent":        "curl/8.0.0",
				"X-App":             "cli",
				"anthropic-beta":    "claude-code-20250219",
				"anthropic-version": "2023-06-01",
			},
			body: validBody(string(monitorPrompt)),
		},
		{
			name: "missing X-App",
			headers: map[string]string{
				"User-Agent":        validHeaders["User-Agent"],
				"anthropic-beta":    validHeaders["anthropic-beta"],
				"anthropic-version": validHeaders["anthropic-version"],
			},
			body: validBody(string(monitorPrompt)),
		},
		{
			name: "missing anthropic-beta",
			headers: map[string]string{
				"User-Agent":        validHeaders["User-Agent"],
				"X-App":             validHeaders["X-App"],
				"anthropic-version": validHeaders["anthropic-version"],
			},
			body: validBody(string(monitorPrompt)),
		},
		{
			name: "missing anthropic-version",
			headers: map[string]string{
				"User-Agent":     validHeaders["User-Agent"],
				"X-App":          validHeaders["X-App"],
				"anthropic-beta": validHeaders["anthropic-beta"],
			},
			body: validBody(string(monitorPrompt)),
		},
		{
			name:    "missing metadata",
			headers: validHeaders,
			body: map[string]any{
				"model":  "claude-haiku-4-5-20251001",
				"system": []any{map[string]any{"type": "text", "text": string(monitorPrompt)}},
			},
		},
		{
			name:    "invalid metadata user ID",
			headers: validHeaders,
			body: func() map[string]any {
				body := validBody(string(monitorPrompt))
				body["metadata"] = map[string]any{"user_id": "invalid"}
				return body
			}(),
		},
		{
			name:       "unrelated prompt",
			headers:    validHeaders,
			body:       validBody("You are a different security classifier for coding agents."),
			wantAccept: false,
		},
		{
			name:       "opening sentence alone",
			headers:    validHeaders,
			body:       validBody(ClaudeCodeSecurityMonitorPrefix),
			wantAccept: false,
		},
		{
			name:    "opening sentence plus arbitrary altered suffix",
			headers: validHeaders,
			body: validBody(ClaudeCodeSecurityMonitorPrefix + "\n\n" +
				strings.Repeat("This is arbitrary altered classifier content. ", 300)),
			wantAccept: false,
		},
		{
			name:    "classifier with trailing session context entry",
			headers: validHeaders,
			body: func() map[string]any {
				body := validBody(string(monitorPrompt))
				// 先断言夹具类型，结构不符时报告测试失败。
				system, ok := body["system"].([]any)
				require.True(t, ok)
				body["system"] = append(system, map[string]any{
					"type": "text",
					"text": sessionContext,
				})
				return body
			}(),
			wantAccept: true,
		},
		{
			name:    "classifier with leading session context entry",
			headers: validHeaders,
			body: func() map[string]any {
				body := validBody(string(monitorPrompt))
				system, ok := body["system"].([]any)
				require.True(t, ok)
				body["system"] = append([]any{map[string]any{
					"type": "text",
					"text": sessionContext,
				}}, system...)
				return body
			}(),
			wantAccept: true,
		},
		{
			name:       "session context entry alone",
			headers:    validHeaders,
			body:       validBody(sessionContext),
			wantAccept: false,
		},
		{
			name:    "tampered classifier with session context entry",
			headers: validHeaders,
			body: func() map[string]any {
				body := validBody(strings.ReplaceAll(
					string(monitorPrompt), "## HARD BLOCK", "## ALTERED BLOCK"))
				system, ok := body["system"].([]any)
				require.True(t, ok)
				body["system"] = append(system, map[string]any{
					"type": "text",
					"text": sessionContext,
				})
				return body
			}(),
			wantAccept: false,
		},
	}

	validator := NewClaudeCodeValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
			for name, value := range tt.headers {
				req.Header.Set(name, value)
			}

			require.Equal(t, tt.wantAccept, validator.Validate(claudeCodeInputFixture(req), tt.body))
		})
	}
}

func TestClaudeCodeValidator_BillingBlockVSCodeEntrypointRecognized(t *testing.T) {
	// Claude Code 的 VSCode 扩展使用 cc_entrypoint=claude-vscode，安全监视器请求也可能缺少身份文本。
	// 此例检查识别接受不同入口值，固定匹配 cli 会误拒该请求。
	monitorPrompt, err := os.ReadFile("testdata/security_monitor_system_prompt.txt")
	require.NoError(t, err)

	validator := NewClaudeCodeValidator()

	// 监视器正文与身份说明文本的 Dice 相似度低于阈值，此例由计费归因块识别放行。
	require.Less(t, validator.BestSimilarityScore(string(monitorPrompt)), ClaudeCodeSystemPromptThreshold)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.181 (external, claude-vscode, agent-sdk/0.3.181)")
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model": "claude-opus-4-8",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "x-anthropic-billing-header: cc_version=2.1.181.f17; cc_entrypoint=claude-vscode;",
			},
			map[string]any{
				"type": "text",
				"text": string(monitorPrompt),
			},
		},
		"metadata": map[string]any{
			"user_id": claudeCodeMetadataUserIDJSON,
		},
	})
	require.True(t, ok)
}

func TestClaudeCodeValidator_BillingBlockWithoutEntrypointFallsThrough(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.162 (external, cli)")
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	// 缺少 cc_entrypoint 字段和身份文本时，请求回到 Dice 检查并被拒绝。
	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model": "claude-3-5-haiku-20241022",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "x-anthropic-billing-header: cc_version=2.1.162.884; cch=d8726;",
			},
			map[string]any{
				"type": "text",
				"text": "Some unrelated system prompt that does not resemble Claude Code.",
			},
		},
		"metadata": map[string]any{
			"user_id": claudeCodeMetadataUserIDJSON,
		},
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_BillingBlockStillRequiresClaudeCodeUA(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "curl/8.0.0")
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	// 计费块无法绕过 User-Agent 校验：非 claude-cli 客户端在第一步即被拒。
	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model": "claude-3-5-haiku-20241022",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "x-anthropic-billing-header: cc_version=2.1.162.884; cc_entrypoint=cli; cch=d8726;",
			},
		},
	})
	require.False(t, ok)
}

// TestClaudeCodeValidator_BillingBlockRecognizedWithoutCCH 检查省略 cch 签名字段的 billing block，格式为
// `x-anthropic-billing-header: cc_version=...; cc_entrypoint=cli;`（无 cch）。
// 检测使用计费前缀和 cc_entrypoint 字段，缺少 cch 和身份文本的子请求仍可识别。
// 本测试同时覆盖 buildBillingAttributionText 注入的计费块格式。
func TestClaudeCodeValidator_BillingBlockRecognizedWithoutCCH(t *testing.T) {
	monitorPrompt, err := os.ReadFile("testdata/security_monitor_system_prompt.txt")
	require.NoError(t, err)

	validator := NewClaudeCodeValidator()
	require.Less(t, validator.BestSimilarityScore(string(monitorPrompt)), ClaudeCodeSystemPromptThreshold)

	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.162 (external, cli)")
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model": "claude-3-5-haiku-20241022",
		"system": []any{
			map[string]any{
				"type": "text",
				// 计费归因块省略 cch 字段。
				"text": "x-anthropic-billing-header: cc_version=2.1.162.884; cc_entrypoint=cli;",
			},
			map[string]any{
				"type": "text",
				"text": string(monitorPrompt),
			},
		},
		"metadata": map[string]any{
			"user_id": claudeCodeMetadataUserIDJSON,
		},
	})
	require.True(t, ok, "无 cch 的新版 billing block 仍应被识别为 Claude Code")
}

// TestClaudeCodeValidator_NoCCHBlockStillRequiresClaudeCodeUA 检查缺少 cch 的请求仍通过 claude-cli UA 校验。
// 其他 UA 即使携带 billing block，也在第一步被拒绝。
func TestClaudeCodeValidator_NoCCHBlockStillRequiresClaudeCodeUA(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "curl/8.0.0")
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model": "claude-3-5-haiku-20241022",
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "x-anthropic-billing-header: cc_version=2.1.162.884; cc_entrypoint=cli;",
			},
		},
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_MessagesPathRejectsNonClaudeCodeUA(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "curl/8.0.0")
	req.Header.Set("X-App", "claude-code")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model":  "claude-opus-4-8",
		"stream": true,
		"system": []any{
			map[string]any{
				"type": "text",
				"text": "You are Claude Code, Anthropic's official CLI for Claude.",
			},
		},
		"metadata": map[string]any{
			"user_id": "user_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_account__session_aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		},
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_MessagesPathWithoutSystemPromptStillRejected(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/messages", nil)
	req.Header.Set("User-Agent", "claude-cli/2.1.156 (Claude Code)")
	req.Header.Set("X-App", "claude-code")
	req.Header.Set("anthropic-beta", "claude-code-20250219")
	req.Header.Set("anthropic-version", "2023-06-01")

	ok := validator.Validate(claudeCodeInputFixture(req), map[string]any{
		"model":  "claude-opus-4-8",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"metadata": map[string]any{
			"user_id": "user_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_account__session_aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		},
	})
	require.False(t, ok)
}

func TestClaudeCodeValidator_NonMessagesPathUAOnly(t *testing.T) {
	validator := NewClaudeCodeValidator()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/models", nil)
	req.Header.Set("User-Agent", "claude-cli/1.2.3 (darwin; arm64)")

	ok := validator.Validate(claudeCodeInputFixture(req), nil)
	require.True(t, ok)
}

// claudeCodeInputFixture 将 HTTP 测试数据转换为客户端识别输入。
func claudeCodeInputFixture(r *http.Request) ClaudeCodeValidationInput {
	bypass, _ := requeststate.IsMaxTokensOneHaikuRequestFromContext(r.Context())
	return ClaudeCodeValidationInput{Path: r.URL.Path, UserAgent: r.Header.Get("User-Agent"), XApp: r.Header.Get("X-App"), AnthropicBeta: r.Header.Get("anthropic-beta"), AnthropicVersion: r.Header.Get("anthropic-version"), MaxTokensOneHaiku: bypass}
}
