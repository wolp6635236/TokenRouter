package provider_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	routing "github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type providerRepoStubForBulkUpdate struct {
	providercore.AdminStore
	bulkUpdateErr     error
	bulkUpdateIDs     []int64
	lastBulkUpdate    providercore.ProviderBulkUpdate
	bindGroupErrByID  map[int64]error
	bindGroupsCalls   []int64
	getByIDsProviders []*providercore.Record
	getByIDsErr       error
	getByIDsCalled    bool
	getByIDsIDs       []int64
	getByIDProviders  map[int64]*providercore.Record
	getByIDErrByID    map[int64]error
	getByIDCalled     []int64
	listByGroupData   map[int64][]providercore.Record
	listByGroupErr    map[int64]error
	listData          []providercore.Record
	listResult        *pagination.PaginationResult
	listErr           error
	listCalled        bool
	lastListParams    pagination.PaginationParams
	lastListFilters   struct {
		platform     string
		providerType string
		status       string
		search       string
		groupID      int64
		privacyMode  string
	}
}

func (s *providerRepoStubForBulkUpdate) BulkUpdate(_ context.Context, ids []int64, updates providercore.ProviderBulkUpdate) (int64, error) {
	s.bulkUpdateIDs = append([]int64{}, ids...)
	s.lastBulkUpdate = updates
	if s.bulkUpdateErr != nil {
		return 0, s.bulkUpdateErr
	}
	return int64(len(ids)), nil
}

func TestAdminServiceBulkUpdateProvidersNormalizesLegacyOpenAIConfiguration(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{
		getByIDsProviders: []*providercore.Record{{
			ID:       1,
			Platform: capability.PlatformOpenAI,
			Type:     capability.ProviderTypeAPIKey,
		}},
	}
	svc := newProviderEditorForTest(repo)
	input := &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1},
		Credentials: map[string]any{
			providercore.LegacyOpenAICapabilitiesCredentialKey: []any{"chat_completions"},
		},
		Extra: map[string]any{
			providercore.LegacyOpenAIResponsesModeExtraKey: "auto",
			"openai_responses_supported":                   false,
		},
	}

	_, err := svc.BulkUpdateProviders(context.Background(), input)

	require.NoError(t, err)
	require.Equal(t, []string{"text_generation"}, repo.lastBulkUpdate.Credentials[providercore.OpenAIWorkloadCapabilitiesCredentialKey])
	require.NotContains(t, repo.lastBulkUpdate.Credentials, providercore.LegacyOpenAICapabilitiesCredentialKey)
	require.Equal(t, "preserve_client_protocol", repo.lastBulkUpdate.Extra["openai_text_route_mode"])
	require.NotContains(t, repo.lastBulkUpdate.Extra, "openai_responses_probe_status")
	require.NotContains(t, repo.lastBulkUpdate.Extra, providercore.LegacyOpenAIResponsesModeExtraKey)
	require.NotContains(t, repo.lastBulkUpdate.Extra, "openai_responses_supported")
}

func TestAdminServiceBulkUpdateProvidersNormalizesOpenAIWorkloadAndTextRoute(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{getByIDsProviders: []*providercore.Record{
		{ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey},
	}}
	svc := newProviderEditorForTest(repo)

	result, err := svc.BulkUpdateProviders(context.Background(), &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1},
		Credentials: map[string]any{
			providercore.OpenAIWorkloadCapabilitiesCredentialKey: []any{"text_generation", "embeddings"},
		},
		Extra: map[string]any{providercore.ExtraKeyTextRouteMode: "force_responses"},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.Equal(t, []string{"text_generation", "embeddings"}, repo.lastBulkUpdate.Credentials[providercore.OpenAIWorkloadCapabilitiesCredentialKey])
	require.Equal(t, "force_responses", repo.lastBulkUpdate.Extra[providercore.ExtraKeyTextRouteMode])
}

func TestAdminServiceBulkUpdateProvidersNormalizesOpenAIContinuationCapability(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{getByIDsProviders: []*providercore.Record{
		{ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey},
	}}
	svc := newProviderEditorForTest(repo)

	result, err := svc.BulkUpdateProviders(context.Background(), &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1},
		Extra: map[string]any{
			providercore.ExtraKeyResponsesContinuationSupported: true,
		},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.Equal(t, true, repo.lastBulkUpdate.Extra[providercore.ExtraKeyResponsesContinuationSupported])
}

func TestAdminServiceBulkUpdateProvidersRejectsContinuationForNonOpenAIAPIKey(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{getByIDsProviders: []*providercore.Record{
		{ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
	}}
	svc := newProviderEditorForTest(repo)

	result, err := svc.BulkUpdateProviders(context.Background(), &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1},
		Extra: map[string]any{
			providercore.ExtraKeyResponsesContinuationSupported: true,
		},
	})

	require.Nil(t, result)
	var appErr *apperror.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "OPENAI_CONFIGURATION_TARGET_INVALID", appErr.Reason)
	require.Empty(t, repo.bulkUpdateIDs)
}

func TestAdminServiceBulkUpdateProvidersRejectsInvalidOpenAITargetBeforeWrite(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{getByIDsProviders: []*providercore.Record{
		{ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth},
	}}
	svc := newProviderEditorForTest(repo)

	result, err := svc.BulkUpdateProviders(context.Background(), &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1},
		Extra:       map[string]any{providercore.ExtraKeyTextRouteMode: "force_responses"},
	})

	require.Nil(t, result)
	var appErr *apperror.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "OPENAI_CONFIGURATION_TARGET_INVALID", appErr.Reason)
	require.Empty(t, repo.bulkUpdateIDs)
}

func TestAdminServiceBulkUpdateProvidersRejectsForcedTextRouteWithoutWorkload(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{getByIDsProviders: []*providercore.Record{
		{ID: 1, Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey},
	}}
	svc := newProviderEditorForTest(repo)

	result, err := svc.BulkUpdateProviders(context.Background(), &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1},
		Credentials: map[string]any{
			providercore.OpenAIWorkloadCapabilitiesCredentialKey: []any{"embeddings"},
		},
		Extra: map[string]any{providercore.ExtraKeyTextRouteMode: "force_responses"},
	})

	require.Nil(t, result)
	var appErr *apperror.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "OPENAI_TEXT_ROUTE_MODE_INVALID", appErr.Reason)
	require.Empty(t, repo.bulkUpdateIDs)
}

func TestAdminServiceBulkUpdateProvidersRejectsInvalidCNProviderCombination(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{
		getByIDsProviders: []*providercore.Record{{
			ID:       9,
			Platform: capability.PlatformDeepseek,
			Type:     capability.ProviderTypeAPIKey,
			Credentials: map[string]any{
				"api_key":       "sk-test",
				"provider_mode": providercore.ProviderModePayG,
			},
		}},
	}
	svc := newProviderEditorForTest(repo)
	_, err := svc.BulkUpdateProviders(context.Background(), &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{9},
		Credentials: map[string]any{"provider_mode": providercore.ProviderModeCoding},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "DeepSeek does not support coding")
	require.Empty(t, repo.bulkUpdateIDs)
}

func (s *providerRepoStubForBulkUpdate) BindGroups(_ context.Context, providerID int64, _ []int64) error {
	s.bindGroupsCalls = append(s.bindGroupsCalls, providerID)
	if err, ok := s.bindGroupErrByID[providerID]; ok {
		return err
	}
	return nil
}

func (s *providerRepoStubForBulkUpdate) GetByIDs(_ context.Context, ids []int64) ([]*providercore.Record, error) {
	s.getByIDsCalled = true
	s.getByIDsIDs = append([]int64{}, ids...)
	if s.getByIDsErr != nil {
		return nil, s.getByIDsErr
	}
	out := make([]*providercore.Record, len(s.getByIDsProviders))
	for i, v := range s.getByIDsProviders {
		out[i] = providercore.CloneRecord(v)
	}
	return out, nil
}

func (s *providerRepoStubForBulkUpdate) GetByID(_ context.Context, id int64) (*providercore.Record, error) {
	s.getByIDCalled = append(s.getByIDCalled, id)
	if err, ok := s.getByIDErrByID[id]; ok {
		return nil, err
	}
	if provider, ok := s.getByIDProviders[id]; ok {
		return providercore.CloneRecord(provider), nil
	}
	return nil, errors.New("provider not found")
}

func (s *providerRepoStubForBulkUpdate) ListByGroup(_ context.Context, groupID int64) ([]providercore.Record, error) {
	if err, ok := s.listByGroupErr[groupID]; ok {
		return nil, err
	}
	if rows, ok := s.listByGroupData[groupID]; ok {
		return rows, nil
	}
	return nil, nil
}

func (s *providerRepoStubForBulkUpdate) ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]providercore.Record, error) {
	return nil, nil
}

func (s *providerRepoStubForBulkUpdate) ListWithFilters(_ context.Context, params pagination.PaginationParams, platform, providerType, status, search string, groupID int64, privacyMode string) ([]providercore.Record, *pagination.PaginationResult, error) {
	s.listCalled = true
	s.lastListParams = params
	s.lastListFilters.platform = platform
	s.lastListFilters.providerType = providerType
	s.lastListFilters.status = status
	s.lastListFilters.search = search
	s.lastListFilters.groupID = groupID
	s.lastListFilters.privacyMode = privacyMode
	if s.listErr != nil {
		return nil, nil, s.listErr
	}
	if s.listResult != nil {
		return s.listData, s.listResult, nil
	}
	return s.listData, &pagination.PaginationResult{Total: int64(len(s.listData))}, nil
}

// TestAdminService_BulkUpdateProviders_AllSuccessIDs 验证批量更新成功时返回 success_ids/failed_ids。
func TestAdminService_BulkUpdateProviders_AllSuccessIDs(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{}
	svc := newProviderEditorForTest(repo)

	schedulable := true
	input := &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1, 2, 3},
		Schedulable: &schedulable,
	}

	result, err := svc.BulkUpdateProviders(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 3, result.Success)
	require.Equal(t, 0, result.Failed)
	require.ElementsMatch(t, []int64{1, 2, 3}, result.SuccessIDs)
	require.Empty(t, result.FailedIDs)
	require.Len(t, result.Results, 3)
}

// TestAdminService_BulkUpdateProviders_PartialFailureIDs 验证部分失败时 success_ids/failed_ids 正确。
func TestAdminService_BulkUpdateProviders_PartialFailureIDs(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{
		bindGroupErrByID: map[int64]error{
			2: errors.New("bind failed"),
		},
	}
	svc := newProviderEditorForTest(repo, shadowGroupsFixture{&bulkGroupsFixture{group: &routing.Group{ID: 10, Name: "g10"}}})

	groupIDs := []int64{10}
	schedulable := false
	input := &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1, 2, 3},
		GroupIDs:    &groupIDs,
		Schedulable: &schedulable,
	}

	result, err := svc.BulkUpdateProviders(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 2, result.Success)
	require.Equal(t, 1, result.Failed)
	require.ElementsMatch(t, []int64{1, 3}, result.SuccessIDs)
	require.ElementsMatch(t, []int64{2}, result.FailedIDs)
	require.Len(t, result.Results, 3)
}

func TestAdminService_BulkUpdateProviders_NilGroupRepoReturnsError(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{}
	svc := newProviderEditorForTest(repo)

	groupIDs := []int64{10}
	input := &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1},
		GroupIDs:    &groupIDs,
	}

	result, err := svc.BulkUpdateProviders(context.Background(), input)
	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "group repository not configured")
}

func TestAdminServiceBulkUpdateProvidersRejectsGeminiThirdPartyWithoutCustomBaseURL(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{
		getByIDsProviders: []*providercore.Record{
			{
				ID:       1,
				Platform: capability.PlatformGemini,
				Type:     capability.ProviderTypeAPIKey,
				Credentials: map[string]any{
					"base_url": "https://generativelanguage.googleapis.com",
				},
			},
		},
	}
	svc := newProviderEditorForTest(repo)

	result, err := svc.BulkUpdateProviders(context.Background(), &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1},
		Credentials: map[string]any{
			providercore.GeminiProviderTypeCredentialKey: providercore.GeminiProviderTypeThirdParty,
		},
	})

	require.Nil(t, result)
	require.ErrorContains(t, err, "GEMINI_THIRD_PARTY_BASE_URL_REQUIRED")
	require.Empty(t, repo.bulkUpdateIDs)
}

// TestAdminServiceBulkUpdateAllowsMixedProviderPlatforms 检查批量更新允许关联已有其他平台提供商的分组。
func TestAdminServiceBulkUpdateAllowsMixedProviderPlatforms(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{
		getByIDsProviders: []*providercore.Record{
			{ID: 1, Platform: capability.PlatformAntigravity},
		},
		// Group 10 already contains an Anthropic provider.
		listByGroupData: map[int64][]providercore.Record{
			10: {{ID: 99, Platform: capability.PlatformAnthropic}},
		},
	}
	svc := newProviderEditorForTest(repo, shadowGroupsFixture{&bulkGroupsFixture{group: &routing.Group{ID: 10, Name: "target-group"}}})

	groupIDs := []int64{10}
	input := &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{1},
		GroupIDs:    &groupIDs,
	}

	result, err := svc.BulkUpdateProviders(context.Background(), input)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.Success)
	require.Len(t, repo.bindGroupsCalls, 1)
}

func TestAdminServiceBulkUpdateProviders_ResolvesIDsFromFilters(t *testing.T) {
	repo := &providerRepoStubForBulkUpdate{
		listData: []providercore.Record{
			{ID: 7},
			{ID: 11},
		},
		listResult: &pagination.PaginationResult{Total: 2},
	}
	svc := newProviderEditorForTest(repo)

	schedulable := true
	input := &providercore.BulkUpdateProvidersInput{
		Schedulable: &schedulable,
	}

	filtersField := reflect.ValueOf(input).Elem().FieldByName("Filters")
	require.True(t, filtersField.IsValid(), "BulkUpdateProvidersInput should expose Filters for filter-target bulk update")
	require.Equal(t, reflect.Pointer, filtersField.Kind(), "BulkUpdateProvidersInput.Filters should be a pointer field")

	filtersValue := reflect.New(filtersField.Type().Elem())
	filtersValue.Elem().FieldByName("Platform").SetString(capability.PlatformOpenAI)
	filtersValue.Elem().FieldByName("Type").SetString(capability.ProviderTypeOAuth)
	filtersValue.Elem().FieldByName("Status").SetString(billing.StatusActive)
	filtersValue.Elem().FieldByName("Group").SetString("12")
	filtersValue.Elem().FieldByName("PrivacyMode").SetString(openai.PrivacyModeCFBlocked)
	filtersValue.Elem().FieldByName("Search").SetString("bulk-target")
	filtersField.Set(filtersValue)

	result, err := svc.BulkUpdateProviders(context.Background(), input)
	require.NoError(t, err)
	require.True(t, repo.listCalled, "expected filter-target bulk update to resolve matching IDs via provider list filters")
	require.Equal(t, capability.PlatformOpenAI, repo.lastListFilters.platform)
	require.Equal(t, capability.ProviderTypeOAuth, repo.lastListFilters.providerType)
	require.Equal(t, billing.StatusActive, repo.lastListFilters.status)
	require.Equal(t, "bulk-target", repo.lastListFilters.search)
	require.Equal(t, int64(12), repo.lastListFilters.groupID)
	require.Equal(t, openai.PrivacyModeCFBlocked, repo.lastListFilters.privacyMode)
	require.Equal(t, []int64{7, 11}, repo.bulkUpdateIDs)
	require.Equal(t, 2, result.Success)
	require.Equal(t, 0, result.Failed)
	require.Equal(t, []int64{7, 11}, result.SuccessIDs)
}

// bulkGroupsFixture 保留批量绑定验证所需的分组存在性读取。
type bulkGroupsFixture struct {
	routing.GroupRepository
	group *routing.Group
}

func (s *bulkGroupsFixture) GetByID(context.Context, int64) (*routing.Group, error) {
	return routing.CloneGroup(s.group), nil
}
