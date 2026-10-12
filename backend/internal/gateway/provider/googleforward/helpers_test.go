package googleforward

// 这些导出方法供测试调用包内实现。
var (
	ResolveProjectForTest      = resolveAntigravityProjectID
	EnsureSignatureForTest     = ensureGeminiFunctionCallThoughtSignatures
	ConvertClaudeForTest       = convertClaudeMessagesToGeminiGenerateContent
	GeminiResponseForTest      = (*Gemini).geminiResponseAdapter
	AntigravityResponseForTest = (*Antigravity).antigravityResponseAdapter
	ErrorBodyLimitForTest      = (*Antigravity).upstreamErrorBodyReadLimit
	GeminiFailoverForTest      = (*Gemini).shouldFailoverGeminiUpstreamError
	GeminiPolicyForTest        = (*Gemini).applyGeminiUpstreamErrorPolicy
	AntigravityBodyForTest     = (*Antigravity).buildAntigravityCompatGeminiBody
)

// AttemptForTest 允许断言状态复位及观测结果，仍使用同一实际状态类型。
type AttemptForTest = attempt

func (a *attempt) ObserveImagesForTest(body []byte) { a.observeImages(body) }

func (a *attempt) ImageCountForTest(original, mapped string) int {
	return a.imageCount(original, mapped)
}
