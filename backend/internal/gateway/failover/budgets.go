package failover

const (
	// OAuth429MaxProviderAttempts 保留 OpenAI 同提供商窗口耗尽后的提供商总预算。
	OAuth429MaxProviderAttempts   = 3
	OAuth429StormMinSwitches      = 1
	FirstOutputTimeoutMaxSwitches = 1
)

// OAuth429State 保存当前请求内 Grok 后续尝试的 429 状态。
type OAuth429State struct{ grokFollowupPending bool }

// OAuth429Provider 保存调用方确定的提供商认证类别。
type OAuth429Provider struct{ OpenAI, Grok bool }

// StopOAuth429 保留 OpenAI 和 Grok 不同的后续预算及无状态兼容路径。
func StopOAuth429(provider OAuth429Provider, status, failedSwitches int, state *OAuth429State) bool {
	if failedSwitches < OAuth429StormMinSwitches {
		return false
	}
	if state != nil && state.grokFollowupPending {
		return true
	}
	if provider.Grok {
		if state == nil {
			return status == 429 && failedSwitches >= 2
		}
		if status == 429 {
			state.grokFollowupPending = true
		}
		return false
	}
	if status != 429 || !provider.OpenAI {
		return false
	}
	return failedSwitches >= OAuth429MaxProviderAttempts
}

// FirstOutputExhausted 累计符合首输出恢复条件的错误，判断是否耗尽预算。
func FirstOutputExhausted(eligible bool, switches *int) bool {
	if !eligible || switches == nil {
		return false
	}
	if *switches >= FirstOutputTimeoutMaxSwitches {
		return true
	}
	*switches++
	return false
}
