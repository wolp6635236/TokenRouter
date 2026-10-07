package billing

// newEligibilityForTest 构造资金准入检查器，用 goroutine 执行缓存回填。
func newEligibilityForTest(cache BillingCache, users BalanceReader, keys APIKeyRateLimitLoader, options *EligibilityOptions) *Eligibility {
	return NewEligibility(cache, users, keys, func() EligibilityOptions { return *options }, nil, func(_ string, fn func()) { go fn() })
}
