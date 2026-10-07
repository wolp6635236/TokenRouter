package billing_test

import (
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/stretchr/testify/require"
)

func TestCalculateSearchCost(t *testing.T) {
	t.Parallel()
	s := billingtestkit.Calculator(nil, map[string]*pricing.ModelPricing{})
	require.Equal(t, 0.0, s.CalculateSearchCost(0, floatPtr(10), 1).ActualCost)
	// 配置和目录均缺价时沿用零成本记录，不生成默认金额。
	require.Zero(t, s.CalculateSearchCost(5, nil, 1).ActualCost)
	// 显式配置为零表示免费。
	require.Equal(t, 0.0, s.CalculateSearchCost(5, floatPtr(0), 1).ActualCost)
	price := 10.0
	cost := s.CalculateSearchCost(100, &price, 1.5)
	// 计算方式为 10 / 1000 * 100 * 1.5 = 1.5。
	require.InDelta(t, 1.0, cost.TotalCost, 1e-9)
	require.InDelta(t, 1.5, cost.ActualCost, 1e-9)
}

func TestCalculateAudioCost(t *testing.T) {
	t.Parallel()
	s := billingtestkit.Calculator(nil, map[string]*pricing.ModelPricing{})
	rt, tts, stt := 0.10, 15.0, 0.50
	cfg := &pricing.AudioPriceConfig{RealtimePerMin: &rt, TTSPerMChars: &tts, STTPerHour: &stt}
	require.InDelta(t, 0.20, s.CalculateAudioCost("realtime", 2, cfg, 1).ActualCost, 1e-9)
	require.InDelta(t, 1.5, s.CalculateAudioCost("tts", 0.1, cfg, 1).ActualCost, 1e-9)
	require.InDelta(t, 0.25, s.CalculateAudioCost("stt", 0.5, cfg, 1).ActualCost, 1e-9)
	require.Equal(t, 0.0, s.CalculateAudioCost("unknown", 1, cfg, 1).ActualCost)
	// 没有操作价格目录时各模式均不猜默认金额。
	require.Zero(t, s.CalculateAudioCost("realtime", 1, nil, 1).ActualCost)
	require.Zero(t, s.CalculateAudioCost("tts", 1, nil, 1).ActualCost)
	require.Zero(t, s.CalculateAudioCost("stt", 1, nil, 1).ActualCost)
	// 显式配置为零表示免费。
	zero := 0.0
	require.Equal(t, 0.0, s.CalculateAudioCost("realtime", 1, &pricing.AudioPriceConfig{RealtimePerMin: &zero}, 1).ActualCost)
}

func floatPtr(v float64) *float64 { return &v }
