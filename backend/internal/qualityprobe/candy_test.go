//go:build unit

package qualityprobe

import "testing"

func TestHasStandalone21(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "独立答案", text: "答案是 21 颗。", want: true},
		{name: "句首", text: "21", want: true},
		{name: "被更大数字吞掉", text: "一共 210 颗", want: false},
		{name: "前缀数字", text: "121 不对", want: false},
		{name: "空文本", text: "", want: false},
		{name: "中文夹数字", text: "最少取出21个糖果", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasStandalone21(tt.text); got != tt.want {
				t.Fatalf("HasStandalone21(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
