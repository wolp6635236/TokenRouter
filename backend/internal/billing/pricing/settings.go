package pricing

import (
	"strings"
	"time"
)

// BillingSettings 是共享价格配置的计费策略，独立于模型价卡是否命中。
type BillingSettings struct {
	PeakRateEnabled              bool     `json:"peak_rate_enabled"`
	PeakStart                    string   `json:"peak_start"`
	PeakEnd                      string   `json:"peak_end"`
	PeakRateMultiplier           float64  `json:"peak_rate_multiplier"`
	LongContextPricingEnabled    bool     `json:"long_context_pricing_enabled"`
	FreeOpenAIFast               bool     `json:"free_openai_fast"`
	BatchImageDiscountMultiplier float64  `json:"batch_image_discount_multiplier"`
	BatchImageHoldMultiplier     float64  `json:"batch_image_hold_multiplier"`
	WebSearchPricePerCall        *float64 `json:"web_search_price_per_call"`
	SearchPricePer1k             *float64 `json:"search_price_per_1k"`
	AudioRealtimePricePerMin     *float64 `json:"audio_realtime_price_per_min"`
	AudioTTSPricePerMillionChars *float64 `json:"audio_tts_price_per_million_chars"`
	AudioSTTPricePerHour         *float64 `json:"audio_stt_price_per_hour"`
}

// DefaultBillingSettings 保持未配置时的既有计费规则。
func DefaultBillingSettings() BillingSettings {
	return BillingSettings{
		PeakRateMultiplier:           1,
		LongContextPricingEnabled:    true,
		BatchImageDiscountMultiplier: 0.5,
		BatchImageHoldMultiplier:     0.6,
	}
}

// Clone 隔离价卡缓存和调用方的可空单价。
func (s BillingSettings) Clone() BillingSettings {
	if s.WebSearchPricePerCall != nil {
		v := *s.WebSearchPricePerCall
		s.WebSearchPricePerCall = &v
	}
	if s.SearchPricePer1k != nil {
		v := *s.SearchPricePer1k
		s.SearchPricePer1k = &v
	}
	if s.AudioRealtimePricePerMin != nil {
		v := *s.AudioRealtimePricePerMin
		s.AudioRealtimePricePerMin = &v
	}
	if s.AudioTTSPricePerMillionChars != nil {
		v := *s.AudioTTSPricePerMillionChars
		s.AudioTTSPricePerMillionChars = &v
	}
	if s.AudioSTTPricePerHour != nil {
		v := *s.AudioSTTPricePerHour
		s.AudioSTTPricePerHour = &v
	}
	return s
}

// parseMinutes 把 "HH:MM" 解析为当日分钟数（0..1439），格式非法返回 (0,false)。
func parseMinutes(hhmm string) (int, bool) {
	// 手工解析避免计费热路径反复走 time.Parse；接受集保持与 time.Parse("15:04", s) 一致：
	// 小时允许 1-2 位数字（0..23），分钟必须是 2 位数字（00..59）。
	colon := strings.IndexByte(hhmm, ':')
	if (colon != 1 && colon != 2) || len(hhmm)-colon-1 != 2 {
		return 0, false
	}
	hour := 0
	for i := range colon {
		digit := hhmm[i] - '0'
		if digit > 9 {
			return 0, false
		}
		hour = hour*10 + int(digit)
	}
	minuteTens, minuteOnes := hhmm[colon+1]-'0', hhmm[colon+2]-'0'
	if minuteTens > 9 || minuteOnes > 9 {
		return 0, false
	}
	minute := int(minuteTens)*10 + int(minuteOnes)
	if hour > 23 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

// PeakMultiplierAt 返回指定时刻 now 的高峰因子。
//   - 未启用 / 未配置 / 配置非法（start>=end 或格式错误） / 非高峰时段 → 返回 1.0（安全降级）
//   - 区间为左闭右开 [PeakStart, PeakEnd)，起止时间需要在同一天。
//   - 调用方先将时刻转换到日期对象的时区
//
// 该方法是纯函数，不读取任何外部状态，便于单测。
func (s *BillingSettings) PeakMultiplierAt(now time.Time) float64 {
	if s == nil || !s.PeakRateEnabled || s.PeakStart == "" || s.PeakEnd == "" {
		return 1.0
	}
	start, ok1 := parseMinutes(s.PeakStart)
	end, ok2 := parseMinutes(s.PeakEnd)
	if !ok1 || !ok2 || start >= end {
		return 1.0
	}
	t := now
	cur := t.Hour()*60 + t.Minute()
	if cur >= start && cur < end {
		return s.PeakRateMultiplier
	}
	return 1.0
}
