package billing_test

import (
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	"github.com/stretchr/testify/require"
)

// TestCalculateImageCost_DefaultPricing 测试无分组配置时使用目录的独立按张价格
func TestCalculateImageCost_DefaultPricing(t *testing.T) {
	svc := billingtestkit.Calculator(newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{"gemini-3-pro-image": {CatalogRules: pricing.CatalogRules{ImagePrices: map[string]float64{"1K": 0.134, "2K": 0.201, "4K": 0.268}}, OutputCostPerImage: 0.134, ImagePricePresent: true, Mode: "image_generation", TokenPricingAbsent: true}}}), map[string]*pricing.ModelPricing{}) // 使用完整型号的目录价

	// 2K 尺寸，显式目录价格 $0.201
	cost, mediaErr := svc.CalculateImageCost("gemini-3-pro-image", "2K", 1, 1.0)
	if mediaErr != nil {
		t.Fatal(mediaErr)
	}
	require.InDelta(t, 0.201, cost.TotalCost, 0.0001)
	require.InDelta(t, 0.201, cost.ActualCost, 0.0001)

	// 多张图片
	cost, mediaErr = svc.CalculateImageCost("gemini-3-pro-image", "2K", 3, 1.0)
	if mediaErr != nil {
		t.Fatal(mediaErr)
	}
	require.InDelta(t, 0.603, cost.TotalCost, 0.0001)
}

func TestCalculateImageCost_NormalizesInvalidSizeTo2K(t *testing.T) {
	svc := billingtestkit.Calculator(newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{"gemini-3-pro-image": {CatalogRules: pricing.CatalogRules{ImagePrices: map[string]float64{"1K": 0.134, "2K": 0.201, "4K": 0.268}}, OutputCostPerImage: 0.134, ImagePricePresent: true, Mode: "image_generation", TokenPricingAbsent: true}}}), map[string]*pricing.ModelPricing{})

	for _, imageSize := range []string{"", "auto", "not-a-size"} {
		t.Run(imageSize, func(t *testing.T) {
			cost, mediaErr := svc.CalculateImageCost("gemini-3-pro-image", imageSize, 2, 1.0)
			if mediaErr != nil {
				t.Fatal(mediaErr)
			}
			require.InDelta(t, 0.402, cost.TotalCost, 0.0001)
			require.InDelta(t, 0.402, cost.ActualCost, 0.0001)
		})
	}
}

// TestCalculateImageCost_Explicit4KPrice 测试显式的 4K 目录单价
func TestCalculateImageCost_Explicit4KPrice(t *testing.T) {
	svc := billingtestkit.Calculator(newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{"gemini-3-pro-image": {CatalogRules: pricing.CatalogRules{ImagePrices: map[string]float64{"1K": 0.134, "2K": 0.201, "4K": 0.268}}, OutputCostPerImage: 0.134, ImagePricePresent: true, Mode: "image_generation", TokenPricingAbsent: true}}}), map[string]*pricing.ModelPricing{})

	// 4K 尺寸，显式目录价格 $0.268
	cost, mediaErr := svc.CalculateImageCost("gemini-3-pro-image", "4K", 1, 1.0)
	if mediaErr != nil {
		t.Fatal(mediaErr)
	}
	require.InDelta(t, 0.268, cost.TotalCost, 0.0001)
}

// TestCalculateImageCost_RateMultiplier 测试费率倍数
func TestCalculateImageCost_RateMultiplier(t *testing.T) {
	svc := billingtestkit.Calculator(newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{"gemini-3-pro-image": {CatalogRules: pricing.CatalogRules{ImagePrices: map[string]float64{"1K": 0.134, "2K": 0.201, "4K": 0.268}}, OutputCostPerImage: 0.134, ImagePricePresent: true, Mode: "image_generation", TokenPricingAbsent: true}}}), map[string]*pricing.ModelPricing{})

	// 费率倍数 1.5x
	cost, mediaErr := svc.CalculateImageCost("gemini-3-pro-image", "2K", 1, 1.5)
	if mediaErr != nil {
		t.Fatal(mediaErr)
	}
	require.InDelta(t, 0.201, cost.TotalCost, 0.0001)   // TotalCost = 0.134 * 1.5
	require.InDelta(t, 0.3015, cost.ActualCost, 0.0001) // ActualCost = 0.201 * 1.5

	// 费率倍数 2.0x
	cost, mediaErr = svc.CalculateImageCost("gemini-3-pro-image", "2K", 2, 2.0)
	if mediaErr != nil {
		t.Fatal(mediaErr)
	}
	require.InDelta(t, 0.402, cost.TotalCost, 0.0001)
	require.InDelta(t, 0.804, cost.ActualCost, 0.0001)
}

// TestCalculateImageCost_ZeroCount 测试 imageCount=0
func TestCalculateImageCost_ZeroCount(t *testing.T) {
	svc := billingtestkit.Calculator(newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{"gemini-3-pro-image": {CatalogRules: pricing.CatalogRules{ImagePrices: map[string]float64{"1K": 0.134, "2K": 0.201, "4K": 0.268}}, OutputCostPerImage: 0.134, ImagePricePresent: true, Mode: "image_generation", TokenPricingAbsent: true}}}), map[string]*pricing.ModelPricing{})

	cost, mediaErr := svc.CalculateImageCost("gemini-3-pro-image", "2K", 0, 1.0)
	if mediaErr != nil {
		t.Fatal(mediaErr)
	}
	require.Equal(t, 0.0, cost.TotalCost)
	require.Equal(t, 0.0, cost.ActualCost)
}

// TestCalculateImageCost_NegativeCount 测试 imageCount=-1
func TestCalculateImageCost_NegativeCount(t *testing.T) {
	svc := billingtestkit.Calculator(newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{"gemini-3-pro-image": {CatalogRules: pricing.CatalogRules{ImagePrices: map[string]float64{"1K": 0.134, "2K": 0.201, "4K": 0.268}}, OutputCostPerImage: 0.134, ImagePricePresent: true, Mode: "image_generation", TokenPricingAbsent: true}}}), map[string]*pricing.ModelPricing{})

	cost, mediaErr := svc.CalculateImageCost("gemini-3-pro-image", "2K", -1, 1.0)
	if mediaErr != nil {
		t.Fatal(mediaErr)
	}
	require.Equal(t, 0.0, cost.TotalCost)
	require.Equal(t, 0.0, cost.ActualCost)
}

// TestCalculateImageCost_ZeroRateMultiplier 锁定新行为：倍率 0 直接按 0 计费
// （保存时已强制 > 0；若仍有 0 泄漏到计费层，零消耗比历史的 1.0 更安全）。
func TestCalculateImageCost_ZeroRateMultiplier(t *testing.T) {
	svc := billingtestkit.Calculator(newCatalogFixture(catalogFixture{pricingData: map[string]*pricing.CatalogModelPricing{"gemini-3-pro-image": {CatalogRules: pricing.CatalogRules{ImagePrices: map[string]float64{"1K": 0.134, "2K": 0.201, "4K": 0.268}}, OutputCostPerImage: 0.134, ImagePricePresent: true, Mode: "image_generation", TokenPricingAbsent: true}}}), map[string]*pricing.ModelPricing{})

	cost, mediaErr := svc.CalculateImageCost("gemini-3-pro-image", "2K", 1, 0)
	if mediaErr != nil {
		t.Fatal(mediaErr)
	}
	require.InDelta(t, 0.201, cost.TotalCost, 0.0001)
	require.InDelta(t, 0.0, cost.ActualCost, 1e-10)
}

// TestGetDefaultImagePrice_UnknownModel 验证缺少目录报价时返回缺价错误。
func TestGetDefaultImagePrice_UnknownModel(t *testing.T) {
	svc := billingtestkit.Calculator(nil, nil)
	for _, size := range []string{"1K", "2K", "4K"} {
		cost, err := svc.CalculateImageCost("gemini-3-pro-image", size, 1, 1)
		require.ErrorIs(t, err, pricing.ErrModelPricingUnavailable)
		require.Nil(t, cost)
	}
}
