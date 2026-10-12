package provider

const (
	// BackgroundSkipAsSkipped 计入 skipped（保持当前默认行为）。
	BackgroundSkipAsSkipped BackgroundSkipAction = iota
	// BackgroundSkipAsSuccess 将跳过计入 success，调用方可按统计需要选择。
	BackgroundSkipAsSuccess
)

// BackgroundSkipAction 定义后台刷新服务在“未实际刷新”场景的计数方式。
type BackgroundSkipAction int

// BackgroundRefreshPolicy 描述后台刷新服务的调用侧策略。
type BackgroundRefreshPolicy struct {
	OnLockHeld       BackgroundSkipAction
	OnAlreadyRefresh BackgroundSkipAction
}

func DefaultBackgroundRefreshPolicy() BackgroundRefreshPolicy {
	return BackgroundRefreshPolicy{
		OnLockHeld:       BackgroundSkipAsSkipped,
		OnAlreadyRefresh: BackgroundSkipAsSkipped,
	}
}

func (p BackgroundRefreshPolicy) HandleLockHeld() error {
	if p.OnLockHeld == BackgroundSkipAsSuccess {
		return nil
	}
	return ErrRefreshSkipped
}

func (p BackgroundRefreshPolicy) HandleAlreadyRefreshed() error {
	if p.OnAlreadyRefresh == BackgroundSkipAsSuccess {
		return nil
	}
	return ErrRefreshSkipped
}
