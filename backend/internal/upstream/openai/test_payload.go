package openai

import (
	"strings"
)

// TestResponsesPayload 构造提供商测试与用量探针共用的 Responses 报文。
func TestResponsesPayload(modelID string, prompt string, isOAuth bool) map[string]any {
	testPrompt := strings.TrimSpace(prompt)
	if testPrompt == "" {
		testPrompt = "hi"
	}
	payload := map[string]any{
		"model": modelID,
		"input": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "input_text",
						"text": testPrompt,
					},
				},
			},
		},
		"stream": true,
	}

	// OAuth 使用 ChatGPT 内部 API 时必须关闭服务端存储。
	if isOAuth {
		payload["store"] = false
	}

	// 两类提供商的 Responses 测试均使用 DefaultInstructions。
	payload["instructions"] = DefaultInstructions

	return payload
}

// TestChatCompletionsPayload 构造 Chat Completions 提供商测试报文。
func TestChatCompletionsPayload(modelID string, prompt string) map[string]any {
	testPrompt := strings.TrimSpace(prompt)
	if testPrompt == "" {
		testPrompt = "hi"
	}

	return map[string]any{
		"model": modelID,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": testPrompt,
			},
		},
		"stream": true,
	}
}
