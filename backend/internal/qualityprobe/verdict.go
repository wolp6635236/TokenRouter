package qualityprobe

import "strings"

// ProbeRound 是一次糖果题加 ModelTrace 的结果。
type ProbeRound struct {
	CandyOK          bool
	CandyError       string
	ModelTraceOK     bool
	ModelTraceError  string
	TracePrediction  string
	TraceProbability float64
	Samples          []ProbeSample
}

// HasUpstreamError 在测号通道返回错误时为 true，例如 403、超时。
// 指纹归因失败写在 ModelTraceError 里，不进 Samples[].Error，不算上游报错。
func HasUpstreamError(round ProbeRound) bool {
	for _, sample := range round.Samples {
		if strings.TrimSpace(sample.Error) != "" {
			return true
		}
	}
	if len(round.Samples) == 0 {
		return strings.TrimSpace(round.CandyError) != "" || strings.TrimSpace(round.ModelTraceError) != ""
	}
	return false
}

// IsDegraded 在糖果题和 ModelTrace 都未通过、且本轮没有上游报错时返回 true。
func IsDegraded(round ProbeRound) bool {
	if HasUpstreamError(round) {
		return false
	}
	return !round.CandyOK && !round.ModelTraceOK
}
