// Package provider adapts provider records and runtime dependencies to upstream
// clients for management, probes, and health updates. Platform responses become
// observations consumed by domain services. Provider services own credential
// coordination and health persistence; usage owns internal model accounting.
//
// Entry points:
//   - test_targets.go selects platform probe executors.
//   - upstream_health.go classifies upstream errors and invokes provider health rules.
//   - usage_http.go builds usage requests with proxy and TLS options.
//
// File groups:
//   - openai*.go, codex*.go, claude*.go, and anthropic*.go handle OpenAI and Anthropic authorization, identity, and probes.
//   - antigravity*.go, gemini*.go, and vertex*.go handle Google authorization, models, retries, and credentials.
//   - grok*.go and qoder*.go handle platform authorization, quota, and probes.
//   - text_apikey_test*.go select and execute Kimi, Zhipu, and DeepSeek probes.
//   - test*.go manages probe state, events, and stream output.
//   - system_usage*.go observes internal model exchanges and submits their expenses through the application lifecycle.
//   - native_test_execution.go runs prepared protocol probes; opencode_test_target.go selects OpenCode model routes and authentication.
//   - model*.go and default_models.go handle model catalogues, mappings, and health.
//   - oauth_usage.go, ollama_usage.go, upstream_usage.go, and usage*.go query and parse usage.
//   - refresh_errors.go, managed_refresh_key.go, and privacy.go handle refresh errors, cache keys, and privacy requests.
//   - runtime_status.go, scheduled_plans.go, and scheduler_score.go supply administrative runtime and scheduling data.
package provider
