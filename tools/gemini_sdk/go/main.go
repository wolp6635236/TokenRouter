package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

// main.go validates gateway SSE through the Google Go SDK in an isolated module.
// The caller supplies a local gateway URL, expected outcome, and optional model;
// fixture credentials never select a real provider unless the owning server
// explicitly enables its authorized upstream test.

// main consumes a complete SDK stream and checks its observable outcome.
// The local route fixture owns authentication and provider selection. The process
// exits unsuccessfully for empty synthetic chunks, unexpected errors, or missing output.
func main() {
	if len(os.Args) < 3 {
		panic("usage: gemini-sdk <base-url> <success|error> [model] [prompt]")
	}
	model, prompt := "gemini-2.5-flash", "Reply with exactly OK."
	if len(os.Args) > 3 {
		model = os.Args[3]
	}
	if len(os.Args) > 4 {
		prompt = os.Args[4]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: "fixture-key", Backend: genai.BackendGeminiAPI, HTTPOptions: genai.HTTPOptions{BaseURL: os.Args[1]}})
	if err != nil {
		panic(err)
	}
	chunks, empty := 0, 0
	var totalTokens int32
	var text strings.Builder
	var failure error
	config := &genai.GenerateContentConfig{MaxOutputTokens: 4096, ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: "HIGH"}}
	for response, err := range client.Models.GenerateContentStream(ctx, model, genai.Text(prompt), config) {
		if err != nil {
			failure = err
			break
		}
		chunks++
		if response.UsageMetadata != nil {
			totalTokens = response.UsageMetadata.TotalTokenCount
		}
		if len(response.Candidates) == 0 && response.UsageMetadata == nil && response.PromptFeedback == nil && response.ResponseID == "" && response.ModelVersion == "" && response.ModelStatus == nil && response.CreateTime.IsZero() {
			empty++
		}
		for _, candidate := range response.Candidates {
			if candidate.Content == nil {
				continue
			}
			for _, part := range candidate.Content.Parts {
				if !part.Thought {
					text.WriteString(part.Text)
				}
			}
		}
	}
	wantError := os.Args[2] == "error"
	var apiError genai.APIError
	apiFailure := errors.As(failure, &apiError) && apiError.Code >= 400
	ok := empty == 0 && ((wantError && apiFailure) || (!wantError && failure == nil && text.Len() > 0))
	message := ""
	if failure != nil {
		message = failure.Error()
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": ok, "chunks": chunks, "empty_chunks": empty, "text_bytes": text.Len(), "error": message, "error_code": apiError.Code, "total_tokens": totalTokens})
	if !ok {
		fmt.Fprintln(os.Stderr, "SDK outcome did not match the fixture")
		os.Exit(1)
	}
}
