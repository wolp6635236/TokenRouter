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
			name:  "糖果题请求失败视为未通过",
			round: ProbeRound{CandyOK: false, CandyError: "timeout", ModelTraceOK: false},
			want:  true,
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
