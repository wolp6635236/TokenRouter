package routing

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	infraerrors "github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
)

// PriceUpdate 区分不修改、清除覆盖与显式零价。
type PriceUpdate struct {
	Set   bool
	Value *float64
}

// BillingSettingsPatch 仅应用本次请求显式提供的配置项。
type BillingSettingsPatch struct {
	PeakRateEnabled              *bool
	PeakStart                    *string
	PeakEnd                      *string
	PeakRateMultiplier           *float64
	LongContextPricingEnabled    *bool
	FreeOpenAIFast               *bool
	BatchImageDiscountMultiplier *float64
	BatchImageHoldMultiplier     *float64
	WebSearchPricePerCall        PriceUpdate
	SearchPricePer1k             PriceUpdate
	AudioRealtimePricePerMin     PriceUpdate
	AudioTTSPricePerMillionChars PriceUpdate
	AudioSTTPricePerHour         PriceUpdate
}

func (p BillingSettingsPatch) Apply(s *pricing.BillingSettings) error {
	if p.PeakRateEnabled != nil {
		s.PeakRateEnabled = *p.PeakRateEnabled
	}
	if p.PeakStart != nil {
		s.PeakStart = *p.PeakStart
	}
	if p.PeakEnd != nil {
		s.PeakEnd = *p.PeakEnd
	}
	if p.PeakRateMultiplier != nil {
		s.PeakRateMultiplier = *p.PeakRateMultiplier
	}
	if p.LongContextPricingEnabled != nil {
		s.LongContextPricingEnabled = *p.LongContextPricingEnabled
	}
	if p.FreeOpenAIFast != nil {
		s.FreeOpenAIFast = *p.FreeOpenAIFast
	}
	if p.BatchImageDiscountMultiplier != nil {
		s.BatchImageDiscountMultiplier = *p.BatchImageDiscountMultiplier
	}
	if p.BatchImageHoldMultiplier != nil {
		s.BatchImageHoldMultiplier = *p.BatchImageHoldMultiplier
	}
	if p.WebSearchPricePerCall.Set {
		s.WebSearchPricePerCall = p.WebSearchPricePerCall.Value
	}
	if p.SearchPricePer1k.Set {
		s.SearchPricePer1k = p.SearchPricePer1k.Value
	}
	if p.AudioRealtimePricePerMin.Set {
		s.AudioRealtimePricePerMin = p.AudioRealtimePricePerMin.Value
	}
	if p.AudioTTSPricePerMillionChars.Set {
		s.AudioTTSPricePerMillionChars = p.AudioTTSPricePerMillionChars.Value
	}
	if p.AudioSTTPricePerHour.Set {
		s.AudioSTTPricePerHour = p.AudioSTTPricePerHour.Value
	}

	for _, v := range []float64{s.PeakRateMultiplier, s.BatchImageDiscountMultiplier, s.BatchImageHoldMultiplier} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return infraerrors.BadRequest("INVALID_BILLING_SETTINGS", "倍率必须为非负有限数值")
		}
	}
	for _, v := range []*float64{s.WebSearchPricePerCall, s.SearchPricePer1k, s.AudioRealtimePricePerMin, s.AudioTTSPricePerMillionChars, s.AudioSTTPricePerHour} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
			return infraerrors.BadRequest("INVALID_BILLING_SETTINGS", "价格必须为非负有限数值")
		}
	}
	if s.BatchImageHoldMultiplier < s.BatchImageDiscountMultiplier {
		return infraerrors.BadRequest("INVALID_BILLING_SETTINGS", "预扣倍率不能低于折扣倍率")
	}
	s.PeakRateEnabled, s.PeakStart, s.PeakEnd, s.PeakRateMultiplier = NormalizePeakRateConfig(s.PeakRateEnabled, s.PeakStart, s.PeakEnd, s.PeakRateMultiplier)
	if err := ValidatePeakRateConfig(s.PeakRateEnabled, s.PeakStart, s.PeakEnd, s.PeakRateMultiplier); err != nil {
		return infraerrors.BadRequest("INVALID_BILLING_SETTINGS", err.Error())
	}
	*s = s.Clone()
	return nil
}

// ParseMinutes 把 "HH:MM" 解析为当日分钟数（0..1439），格式非法返回 (0,false)。
func ParseMinutes(hhmm string) (int, bool) {
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

// ValidatePeakRateConfig 是高峰倍率配置的唯一校验来源，供 handler 与 service 层共用。
// enabled=true 时要求 start/end 合法且 end>start（不支持跨天），multiplier>=0。
// multiplier=0 是允许的，表示高峰 token 请求按 0 倍计费，可用于折扣/免费策略。
// enabled=false 时放行。
func ValidatePeakRateConfig(enabled bool, start, end string, multiplier float64) error {
	if !enabled {
		return nil
	}
	if start == "" || end == "" {
		return errors.New("peak_rate_enabled 为 true 时 peak_start 与 peak_end 必填")
	}
	st, okStart := ParseMinutes(start)
	if !okStart {
		return fmt.Errorf("peak_start 格式应为 HH:MM，got %q", start)
	}
	en, okEnd := ParseMinutes(end)
	if !okEnd {
		return fmt.Errorf("peak_end 格式应为 HH:MM，got %q", end)
	}
	if st >= en {
		return errors.New("peak_end 必须大于 peak_start（不支持跨天区间，如 22:00-02:00）")
	}
	if multiplier < 0 {
		return errors.New("peak_rate_multiplier 不能为负")
	}
	return nil
}

// NormalizePeakRateConfig 归一化最终落库的高峰倍率配置，供价格配置创建与更新共用。
// 启用时保持原值并交给 ValidatePeakRateConfig 严格校验；停用时保留合法窗口与非负倍率，
// 仅清理脏窗口与负倍率，便于管理员临时停用后按原配置重新启用。
func NormalizePeakRateConfig(enabled bool, start, end string, multiplier float64) (bool, string, string, float64) {
	if enabled {
		return enabled, start, end, multiplier
	}
	if _, ok := ParseMinutes(start); !ok {
		start = ""
	}
	if _, ok := ParseMinutes(end); !ok {
		end = ""
	}
	if multiplier < 0 {
		multiplier = 1.0
	}
	return false, start, end, multiplier
}
