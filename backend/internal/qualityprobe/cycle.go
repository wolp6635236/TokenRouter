package qualityprobe

import "time"

// Trigger 区分定时任务和管理员手动探测。
type Trigger string

const (
	TriggerAuto   Trigger = "auto"
	TriggerManual Trigger = "manual"
)

// CycleState 记录某个提供商的连续失败次数和是否已停止自动循环。
type CycleState struct {
	ConsecutiveFails int
	CycleStopped     bool
	NextRetryAt      *time.Time
}

// DecideInput 是一次探测结束后的决策输入。
type DecideInput struct {
	Settings            Settings
	Platform            string
	LastSchedulableHeld bool
	State               CycleState
	Round               ProbeRound
	Trigger             Trigger
}

// Decision 是对调度、邮件和循环状态的处理结果。
type Decision struct {
	Skip                   bool
	Degraded               bool
	UpstreamError          bool
	TempUnschedule         bool
	ClearTemp              bool
	Cooldown               time.Duration
	SendEmail              bool
	StopCycle              bool
	SkipBecauseLastInGroup bool
	ConsecutiveFails       int
}

// Decide 根据本轮探测结果计算停调、邮件和循环是否继续。
func Decide(in DecideInput) Decision {
	if !in.Settings.Enabled {
		return Decision{Skip: true}
	}
	if in.Platform != PlatformOpenAI {
		return Decision{Skip: true}
	}
	if in.Trigger == TriggerAuto && in.State.CycleStopped {
		return Decision{
			Skip:             true,
			StopCycle:        true,
			ConsecutiveFails: in.State.ConsecutiveFails,
		}
	}
	maxAttempts := in.Settings.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	cooldown := in.Settings.Cooldown
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	// 上游报错保留原有失败计数和循环状态，不按降智处理。
	if HasUpstreamError(in.Round) {
		return Decision{
			UpstreamError:    true,
			ConsecutiveFails: in.State.ConsecutiveFails,
			StopCycle:        in.State.CycleStopped,
		}
	}
	if !IsDegraded(in.Round) {
		return Decision{
			ClearTemp: true,
		}
	}
	fails := in.State.ConsecutiveFails + 1
	out := Decision{
		Degraded:         true,
		ConsecutiveFails: fails,
		Cooldown:         cooldown,
		StopCycle:        fails >= maxAttempts,
		SendEmail:        fails == maxAttempts,
	}
	if !in.Settings.UnscheduleOnDegraded {
		return out
	}
	if in.LastSchedulableHeld {
		out.SkipBecauseLastInGroup = true
		return out
	}
	out.TempUnschedule = true
	return out
}
