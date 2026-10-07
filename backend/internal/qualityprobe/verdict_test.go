package qualityprobe

import "testing"

func TestIsDegradedRequiresBothChecksToFail(t *testing.T) {
	tests := []struct {
		name  string
		round ProbeRound
		want  bool
	}{
		{
			name:  "两样都失败",
			round: ProbeRound{CandyOK: false, ModelTraceOK: false},
			want:  true,
		},
		{
			name:  "只有糖果题失败",
			round: ProbeRound{CandyOK: false, ModelTraceOK: true},
			want:  false,
		},
		{
			name:  "只有 ModelTrace 失败",
			round: ProbeRound{CandyOK: true, ModelTraceOK: false},
			want:  false,
		},
		{
			name:  "两样都通过",
			round: ProbeRound{CandyOK: true, ModelTraceOK: true},
			want:  false,
		},
		{
			name: "测号报错不算降智",
			round: ProbeRound{
				CandyOK:      false,
				ModelTraceOK: false,
				Samples: []ProbeSample{
					{Name: "candy", Error: `API returned 403: {"code":"INSUFFICIENT_BALANCE"}`},
					{Name: "trace_1", Error: `API returned 403: {"code":"INSUFFICIENT_BALANCE"}`},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDegraded(tt.round); got != tt.want {
				t.Fatalf("IsDegraded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasUpstreamError(t *testing.T) {
	if !HasUpstreamError(ProbeRound{
		Samples: []ProbeSample{{Name: "candy", Error: "timeout"}},
	}) {
		t.Fatal("sample error should count")
	}
	if HasUpstreamError(ProbeRound{
		ModelTraceError: "need more numbers",
		Samples:         []ProbeSample{{Name: "trace_1", Answer: "1 2 3", OK: false}},
	}) {
		t.Fatal("attribution-only failure is not upstream error")
	}
	if !HasUpstreamError(ProbeRound{CandyError: "prober is missing"}) {
		t.Fatal("empty samples with candy error should count")
	}
}
