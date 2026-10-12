package provider

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/billing/pricing"
	billingprovider "github.com/TokenFlux/TokenRouter/internal/billing/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/modelidentity"
)

const (
	mediaAliasFixture = `{
	"models":{"openai/gpt-image-2":{"name":"Image"}},
	"providers":{
		"openai":{"models":{
			"gpt-image-2":{"cost":{"input":5,"output":30}},
			"gpt-image-2-snapshot":{"canonical_model_id":"openai/gpt-image-2","cost":{"input":4,"output":20}}
		}},
		"openrouter":{"models":{"gpt-image-2":{"canonical_model_id":"openai/gpt-image-2","cost":{"input":1,"output":2}}}}
	}
}`

	modelsCatalogFixture = `{"models":{"anthropic/claude-test":{"name":"Claude"}},"providers":{"anthropic":{"models":{"claude-test":{"name":"Claude","reasoning":true,"limit":{"output":10},"cost":{"input":3,"output":15,"cache_write":3.75,"cache_read":0.3,"tiers":[{"tier":{"type":"context","size":100},"input":6,"output":30,"cache_write":7.5,"cache_read":0.6},{"tier":{"type":"context","size":200},"input":9,"output":45,"cache_write":11.25,"cache_read":0.9}]}},"attributes-only":{"name":"No price","temperature":false}}}}}`
)

// modelCatalogFixture 在服务启动前准备目录选项和模型价格数据。
type modelCatalogFixture struct {
	options     Options
	pricingData map[string]*pricing.CatalogModelPricing
}

type catalogRemoteFixture struct {
	mu         sync.Mutex
	body       []byte
	err        error
	etag       string
	unchanged  bool
	validators []string
}

func newModelCatalogFixture(fixture modelCatalogFixture) *Service {
	options := fixture.options
	options.ModelLookupCandidates = modelidentity.CandidatesFactory
	return NewServiceFromSnapshot(options, nil, Snapshot{Data: fixture.pricingData})
}

func setPricingFixtureData(service *Service, data map[string]*pricing.CatalogModelPricing) {
	service.pricingData = data
}

func newStubCatalogFromJSON(t *testing.T, body string) *Service {
	t.Helper()
	service := newModelCatalogFixture(modelCatalogFixture{})
	data, err := parsePricingFixture([]byte(body))
	require.NoError(t, err)
	setPricingFixtureData(service, data)
	return service
}

// parsePricingFixture 使用生产解析器读取价格测试数据。
func parsePricingFixture(body []byte) (map[string]*pricing.CatalogModelPricing, error) {
	raw, err := pricing.DecodeCatalogEntries(body)
	if err != nil {
		return nil, err
	}
	values, diagnostics, err := pricing.ParsePricingEntries(raw)
	if validationErr := diagnostics.ValidationError(); validationErr != nil {
		return nil, validationErr
	}
	return values, err
}

// newOfflinePricingFixture 加载内嵌目录和官方价格补充。
func newOfflinePricingFixture(t *testing.T) *Service {
	t.Helper()
	service := newModelCatalogFixture(modelCatalogFixture{options: Options{DataDir: t.TempDir()}})
	require.NoError(t, service.Initialize())
	return service
}

// catalogPriceForTest 查询目录并转换成计费价格。
func catalogPriceForTest(t *testing.T, service *Service, model string) *pricing.ModelPricing {
	t.Helper()
	value, err := pricing.ResolveModelPricing(model, service.GetModelPricing(model))
	require.NoError(t, err)
	return value
}

func (r *catalogRemoteFixture) FetchCatalog(_ context.Context, _ string, validator string) ([]byte, string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validators = append(r.validators, validator)
	return r.body, r.etag, r.unchanged, r.err
}

// readCatalogTestFile 读取并发发布测试准备的目录正文。
func readCatalogTestFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return body
}

// newBillingFixture 目录和计费测试组合生产计算器，并注入缺省倍率和时钟。
func newBillingFixture(catalog *Service) *billing.Calculator {
	var source billing.PriceCatalog
	if catalog != nil {
		source = catalog
	}
	return billing.NewCalculator(source, billing.CalculatorOptions{
		Now:          time.Now,
		LoadLocation: billingprovider.LoadPricingLocation,
	})
}
