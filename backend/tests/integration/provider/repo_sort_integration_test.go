//go:build integration

package provider_test

import (
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
)

func (s *ProviderRepoSuite) TestList_DefaultSortByNameAsc() {
	mustCreateProvider(s.T(), s.client, &providercore.Record{Name: "z-provider"})
	mustCreateProvider(s.T(), s.client, &providercore.Record{Name: "a-provider"})

	providers, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, "", "", "", "", 0, "")
	s.Require().NoError(err)
	s.Require().Len(providers, 2)
	s.Require().Equal("a-provider", providers[0].Name)
	s.Require().Equal("z-provider", providers[1].Name)
}

func (s *ProviderRepoSuite) TestListWithFilters_SortByPriorityDesc() {
	mustCreateProvider(s.T(), s.client, &providercore.Record{Name: "low-priority", Priority: 10})
	mustCreateProvider(s.T(), s.client, &providercore.Record{Name: "high-priority", Priority: 90})

	providers, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
		Page:      1,
		PageSize:  10,
		SortBy:    "priority",
		SortOrder: "desc",
	}, "", "", "", "", 0, "")
	s.Require().NoError(err)
	s.Require().Len(providers, 2)
	s.Require().Equal("high-priority", providers[0].Name)
	s.Require().Equal("low-priority", providers[1].Name)
}
