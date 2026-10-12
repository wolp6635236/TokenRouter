package pricing

import (
	"strings"
)

// CatalogQuery 使用同一份完整模型身份查询价格与属性。
type CatalogQuery struct {
	Entries    map[string]*CatalogModelPricing
	Candidates func(string) []string
}

// GetModelPricing 按完整模型身份查询目录，不借用其它型号的价格。
func (s *CatalogQuery) GetModelPricing(modelName string) *CatalogModelPricing {
	candidates := s.modelLookupCandidates(modelName)
	return s.LookupModelCatalogEntry(candidates)
}

// GetModelModalities 从完整型号的目录条目读取模态。
func (s *CatalogQuery) GetModelModalities(modelName string) ([]string, []string) {
	if s == nil {
		return nil, nil
	}

	modelLower := strings.ToLower(strings.TrimSpace(modelName))
	if modelLower == "" {
		return nil, nil
	}

	lookupCandidates := s.modelLookupCandidates(modelLower)
	return DeriveModalities(s.LookupModelCatalogEntry(lookupCandidates))
}

// LookupModelCatalogEntry 按候选顺序查询显式目录；调用期间目录保持只读。
func (s *CatalogQuery) LookupModelCatalogEntry(candidates []string) *CatalogModelPricing {
	for _, candidate := range candidates {
		if pricing := s.Entries[candidate]; pricing != nil {
			return pricing
		}
	}
	return nil
}

func (s *CatalogQuery) modelLookupCandidates(model string) []string {
	if s.Candidates != nil {
		return s.Candidates(model)
	}
	return BuildModelLookupCandidates(model)
}
