package qualityprobe

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

// IsDegraded 在糖果题和 ModelTrace 都未通过时返回 true。
func IsDegraded(round ProbeRound) bool {
	return !round.CandyOK && !round.ModelTraceOK
}
