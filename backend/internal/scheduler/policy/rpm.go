package policy

const (
	RPMAllowed RPMAllowance = iota
	RPMStickyOnly
	RPMBlocked
)

// RPMAllowance 表示允许请求、仅允许粘性请求或拒绝请求三种状态。
type RPMAllowance int

// CheckRPM 在计数达到 base 时限制为粘性请求，达到 base+buffer 时拒绝请求。
// sticky_exempt 策略在超过 base 后仍允许粘性请求。
func CheckRPM(current, base, buffer int, strategy string) RPMAllowance {
	if base <= 0 || current < base {
		return RPMAllowed
	}
	if strategy == "sticky_exempt" {
		return RPMStickyOnly
	}
	if current < base+buffer {
		return RPMStickyOnly
	}
	return RPMBlocked
}

// RPMStickyBuffer 优先使用正数 override，否则将并发数与会话数相加。
// base 为正时，缓冲至少为 base/5 且至少为 1；base 非正时返回零。
func RPMStickyBuffer(base, concurrency, sessions, override int) int {
	if override > 0 {
		return override
	}
	if base <= 0 {
		return 0
	}
	if concurrency < 0 {
		concurrency = 0
	}
	if sessions < 0 {
		sessions = 0
	}
	buffer := concurrency + sessions
	floor := max(base/5, 1)
	if buffer < floor {
		buffer = floor
	}
	return buffer
}
