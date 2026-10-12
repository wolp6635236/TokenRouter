package provider

import (
	"time"
)

const (
	// ProviderRefreshErrorReturn 在刷新失败时返回错误。
	ProviderRefreshErrorReturn ProviderRefreshErrorAction = iota
	// ProviderRefreshErrorUseExistingToken 失败后继续使用现有 token。
	ProviderRefreshErrorUseExistingToken
)

//nolint:decorder // iota 按块内序号计数，每个枚举需要独立的 const 块。
const (
	// ProviderLockHeldUseExistingToken 直接使用现有 token。
	ProviderLockHeldUseExistingToken ProviderLockHeldAction = iota
	// ProviderLockHeldWaitForCache 等待后重试缓存读取。
	ProviderLockHeldWaitForCache
)

// ProviderRefreshErrorAction 定义 provider 在刷新失败时的处理动作。
type ProviderRefreshErrorAction int

// ProviderLockHeldAction 定义 provider 在刷新锁被占用时的处理动作。
type ProviderLockHeldAction int

// ProviderRefreshPolicy 描述 provider 的平台差异策略。
type ProviderRefreshPolicy struct {
	OnRefreshError ProviderRefreshErrorAction
	OnLockHeld     ProviderLockHeldAction
	FailureTTL     time.Duration
}

// ProviderRefreshPlatformPolicy 组合刷新资格判断和错误快照。
func ProviderRefreshPlatformPolicy() RefreshPlatformPolicy {
	return RefreshPlatformPolicy{
		Eligibility: GrokOAuthRequestProviderEligibilityError,
		MissingRefreshToken: func() error {
			return ErrGrokOAuthRefreshTokenMissing
		},
		SnapshotError: WithGrokCredentialFailureSnapshot,
		ConfigurationError: func(err error) error {
			return &ProviderConfigurationRefreshError{Cause: err}
		},
		ContainmentError: func(err error) error {
			return &ProviderCycleContainmentRefreshError{Cause: err}
		},
	}
}

func ClaudeProviderRefreshPolicy() ProviderRefreshPolicy {
	return ProviderRefreshPolicy{
		OnRefreshError: ProviderRefreshErrorUseExistingToken,
		OnLockHeld:     ProviderLockHeldWaitForCache,
		FailureTTL:     time.Minute,
	}
}

func OpenAIProviderRefreshPolicy() ProviderRefreshPolicy {
	return ProviderRefreshPolicy{
		OnRefreshError: ProviderRefreshErrorUseExistingToken,
		OnLockHeld:     ProviderLockHeldWaitForCache,
		FailureTTL:     time.Minute,
	}
}

func GeminiProviderRefreshPolicy() ProviderRefreshPolicy {
	return ProviderRefreshPolicy{
		OnRefreshError: ProviderRefreshErrorReturn,
		OnLockHeld:     ProviderLockHeldUseExistingToken,
		FailureTTL:     0,
	}
}

func AntigravityProviderRefreshPolicy() ProviderRefreshPolicy {
	return ProviderRefreshPolicy{
		OnRefreshError: ProviderRefreshErrorReturn,
		OnLockHeld:     ProviderLockHeldUseExistingToken,
		FailureTTL:     0,
	}
}

func GrokProviderRefreshPolicy() ProviderRefreshPolicy {
	return ProviderRefreshPolicy{
		OnRefreshError: ProviderRefreshErrorReturn,
		OnLockHeld:     ProviderLockHeldWaitForCache,
		FailureTTL:     0,
	}
}
