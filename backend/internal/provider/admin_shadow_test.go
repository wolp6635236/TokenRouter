package provider_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/server/httpx"

	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// sparkShadowRepoStub 是 ProviderRepository 的内存测试桩，
// 专为 CreateShadow 单元测试设计。
// 嵌入 mockProviderRepoForGemini（由 gemini_multiplatform_test.go 提供所有 stub 方法），
// 并覆盖测试所需的核心方法。
type sparkShadowRepoStub struct {
	providercore.AdminStore
	providersByID map[int64]*providercore.Record
	nextID        int64
	providers     map[int64]*providercore.Record
	groupsOf      map[int64][]int64 // providerID → []groupIDs
}

func newSparkShadowRepoStub() *sparkShadowRepoStub {
	return &sparkShadowRepoStub{
		nextID:        0,
		providers:     make(map[int64]*providercore.Record),
		groupsOf:      make(map[int64][]int64),
		providersByID: make(map[int64]*providercore.Record),
	}
}

func (s *sparkShadowRepoStub) Create(_ context.Context, provider *providercore.Record) error {
	s.nextID++
	provider.ID = s.nextID
	cp := *providercore.CloneRecord(provider)
	s.providers[provider.ID] = &cp
	s.providersByID[provider.ID] = &cp
	return nil
}

func (s *sparkShadowRepoStub) GetByID(_ context.Context, id int64) (*providercore.Record, error) {
	acc, ok := s.providers[id]
	if !ok {
		return nil, providercore.ErrProviderNotFound
	}
	return providercore.CloneRecord(acc), nil
}

func (s *sparkShadowRepoStub) ListShadowsByParent(_ context.Context, parentID int64) ([]*providercore.Record, error) {
	var result []*providercore.Record
	for _, acc := range s.providers {
		if acc.ParentProviderID != nil && *acc.ParentProviderID == parentID && acc.QuotaDimension == providercore.QuotaDimensionSpark {
			cp := *providercore.CloneRecord(acc)
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (s *sparkShadowRepoStub) BindGroups(_ context.Context, providerID int64, groupIDs []int64) error {
	s.groupsOf[providerID] = append(s.groupsOf[providerID], groupIDs...)
	return nil
}

func (s *sparkShadowRepoStub) ListSchedulableByGroupID(_ context.Context, groupID int64) ([]providercore.Record, error) {
	var result []providercore.Record
	for accID, groups := range s.groupsOf {
		for _, gid := range groups {
			if gid == groupID {
				if acc, ok := s.providers[accID]; ok {
					result = append(result, *acc)
				}
				break
			}
		}
	}
	return result, nil
}

// ExistsByID ── 追加 stub（ProviderRepository に必要な残りのメソッド）──────────────────
func (s *sparkShadowRepoStub) ExistsByID(_ context.Context, id int64) (bool, error) {
	_, ok := s.providers[id]
	return ok, nil
}

func (s *sparkShadowRepoStub) Update(_ context.Context, provider *providercore.Record) error {
	if _, ok := s.providers[provider.ID]; !ok {
		return providercore.ErrProviderNotFound
	}
	cp := *providercore.CloneRecord(provider)
	s.providers[provider.ID] = &cp
	s.providersByID[provider.ID] = &cp
	return nil
}

func (s *sparkShadowRepoStub) Delete(_ context.Context, id int64) error {
	delete(s.providers, id)
	delete(s.providersByID, id)
	return nil
}

func (s *sparkShadowRepoStub) BatchUpdateLastUsed(_ context.Context, _ map[int64]time.Time) error {
	return nil
}

func (s *sparkShadowRepoStub) ListByGroup(_ context.Context, _ int64) ([]providercore.Record, error) {
	return nil, nil
}

func (s *sparkShadowRepoStub) ListWithFilters(_ context.Context, _ pagination.PaginationParams, _, _, _, _ string, _ int64, _ string) ([]providercore.Record, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

// TestCreateShadow はメインのシナリオを検証する。
//
// 检查影子的 ParentProviderID、QuotaDimension、默认 spark model_mapping、凭据为空及继承 ProxyID。
// 同一母提供商再次创建影子时返回错误。
func TestCreateShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)

	proxyID := int64(7)
	parent := &providercore.Record{
		Name:     "p",
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Status:   billing.StatusActive,
		ProxyID:  &proxyID,
		Credentials: map[string]any{
			"refresh_token":      "RT",
			"chatgpt_account_id": "org-x",
		},
	}
	require.NoError(t, repo.Create(ctx, parent))

	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "p-spark", Priority: 50})
	require.NoError(t, err)
	require.NotNil(t, shadow)
	require.Equal(t, parent.ID, *shadow.ParentProviderID)
	require.Equal(t, providercore.QuotaDimensionSpark, shadow.QuotaDimension)
	require.Equal(t, provideradapter.DefaultSparkShadowModels(), shadow.Credentials["model_mapping"],
		"影子默认带 spark 恒等变体映射")
	require.Nil(t, shadow.Credentials["refresh_token"], "影子不得持有 auth token")
	require.Nil(t, shadow.Credentials["access_token"], "影子不得持有 auth token")
	require.Equal(t, parent.ProxyID, shadow.ProxyID)
	require.NotContains(t, shadow.Extra, "openai_long_context_billing_enabled")

	_, err = svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "dup"})
	require.Error(t, err)
}

// TestCreateShadow_BindGroups は BindGroups の後置呼び出しを検証する。
// 影子提供商が指定グループに属し、ListSchedulableByGroupID で取得可能であること。
func TestCreateShadow_BindGroups(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)

	parent := &providercore.Record{
		Name:     "parent",
		Platform: capability.PlatformOpenAI,
		Type:     capability.ProviderTypeOAuth,
		Status:   billing.StatusActive,
		Credentials: map[string]any{
			"chatgpt_account_id": "org-y",
		},
	}
	require.NoError(t, repo.Create(ctx, parent))

	const testGroupID = int64(42)
	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{
		Name:     "p-spark",
		GroupIDs: []int64{testGroupID},
	})
	require.NoError(t, err)
	require.NotNil(t, shadow)
	require.Equal(t, []int64{testGroupID}, shadow.GroupIDs, "CreateShadow should backfill GroupIDs into the returned shadow")

	providers, err := repo.ListSchedulableByGroupID(ctx, testGroupID)
	require.NoError(t, err)
	require.Len(t, providers, 1)
	require.Equal(t, shadow.ID, providers[0].ID)
}

// TestCreateShadow_InheritsParentConcurrency 验证未指定并发时
// 影子继承母提供商的并发数，限流器将 Concurrency=0 解释为无限并发。
func TestCreateShadow_InheritsParentConcurrency(t *testing.T) {
	ctx := context.Background()

	t.Run("unspecified_inherits_parent", func(t *testing.T) {
		repo := newSparkShadowRepoStub()
		svc := newProviderEditorForTest(repo)
		parent := &providercore.Record{
			Name: "conc-parent", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
			Status: billing.StatusActive, Concurrency: 3,
			Credentials: map[string]any{"chatgpt_account_id": "org-c"},
		}
		require.NoError(t, repo.Create(ctx, parent))

		shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "conc-shadow"})
		require.NoError(t, err)
		require.Equal(t, 3, shadow.Concurrency, "未指定并发应继承母提供商(非 0=无限)")
		require.Equal(t, 3, repo.providers[shadow.ID].Concurrency)
	})

	t.Run("explicit_positive_kept", func(t *testing.T) {
		repo := newSparkShadowRepoStub()
		svc := newProviderEditorForTest(repo)
		parent := &providercore.Record{
			Name: "conc-parent2", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
			Status: billing.StatusActive, Concurrency: 3,
			Credentials: map[string]any{"chatgpt_account_id": "org-c2"},
		}
		require.NoError(t, repo.Create(ctx, parent))

		shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "conc-shadow2", Concurrency: 2})
		require.NoError(t, err)
		require.Equal(t, 2, shadow.Concurrency, "显式正并发应保留")
	})
}

// TestCreateShadow_InheritsParentPriorityWhenOmitted 检查请求省略优先级时继承母提供商的值。
// SetPriority 会覆盖 Ent 默认值 50，数值越小调度越优先，前端仅传 name 时使用继承值。
func TestCreateShadow_InheritsParentPriorityWhenOmitted(t *testing.T) {
	ctx := context.Background()

	t.Run("unspecified_inherits_parent", func(t *testing.T) {
		repo := newSparkShadowRepoStub()
		svc := newProviderEditorForTest(repo)
		parent := &providercore.Record{
			Name: "prio-parent", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
			Status: billing.StatusActive, Priority: 30,
			Credentials: map[string]any{"chatgpt_account_id": "org-p"},
		}
		require.NoError(t, repo.Create(ctx, parent))

		shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "prio-shadow"})
		require.NoError(t, err)
		require.Equal(t, 30, shadow.Priority, "未指定优先级应继承母提供商(而非 0=最高优先级)")
		require.Equal(t, 30, repo.providers[shadow.ID].Priority)
	})

	t.Run("explicit_positive_kept", func(t *testing.T) {
		repo := newSparkShadowRepoStub()
		svc := newProviderEditorForTest(repo)
		parent := &providercore.Record{
			Name: "prio-parent2", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
			Status: billing.StatusActive, Priority: 30,
			Credentials: map[string]any{"chatgpt_account_id": "org-p2"},
		}
		require.NoError(t, repo.Create(ctx, parent))

		shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "prio-shadow2", Priority: 7})
		require.NoError(t, err)
		require.Equal(t, 7, shadow.Priority, "显式正优先级应保留")
	})
}

// TestPersistProviderCredentials_SkipsShadow 验证凭据写入唯一汇聚点
// persistProviderCredentials 对 spark 影子早返 no-op,任何上游路径都无法把凭据落到影子行。
func TestPersistProviderCredentials_SkipsShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	parentID := int64(1)
	shadow := &providercore.Record{
		Name: "shadow", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
		Status: billing.StatusActive, Credentials: map[string]any{},
		ParentProviderID: &parentID, QuotaDimension: providercore.QuotaDimensionSpark,
	}
	require.NoError(t, repo.Create(ctx, shadow))

	_, err := providercore.PersistCredentials(ctx, repo, shadow, map[string]any{"access_token": "LEAK", "refresh_token": "LEAK"}, nil)
	require.NoError(t, err)
	require.Empty(t, shadow.Credentials, "影子凭据不可被写入(传入对象)")
	require.Empty(t, repo.providers[shadow.ID].Credentials, "影子凭据不可被写入(仓储)")
}

// TestResolveCredentialProvider_RejectsParentShadow 检查母提供商也是影子时拒绝凭据解析。
func TestResolveCredentialProvider_RejectsParentShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()

	grandparent := &providercore.Record{
		Name: "gp", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive,
		Credentials: map[string]any{"refresh_token": "RT"},
	}
	require.NoError(t, repo.Create(ctx, grandparent))

	parentShadow := &providercore.Record{
		Name: "parent-shadow", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive,
		Credentials: map[string]any{}, ParentProviderID: &grandparent.ID, QuotaDimension: providercore.QuotaDimensionSpark,
	}
	require.NoError(t, repo.Create(ctx, parentShadow))
	child := &providercore.Record{
		Name: "child-shadow", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive,
		Credentials: map[string]any{}, ParentProviderID: &parentShadow.ID, QuotaDimension: providercore.QuotaDimensionSpark,
	}
	require.NoError(t, repo.Create(ctx, child))

	_, err := providercore.ResolveCredentialRecord(ctx, repo.GetByID, child)
	require.Error(t, err, "父提供商本身是影子时凭据解析应拒绝(fail-closed)")
}

// TestResetProviderQuota_RejectsShadow 检查影子的额度重置返回 400，母提供商可正常重置。
func TestResetProviderQuota_RejectsShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)
	parent := &providercore.Record{
		Name: "rq-parent", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive,
		Credentials: map[string]any{"chatgpt_account_id": "o"},
	}
	require.NoError(t, repo.Create(ctx, parent))
	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "rq-shadow"})
	require.NoError(t, err)

	err = svc.ResetProviderQuota(ctx, shadow.ID)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err), "影子 reset-quota 应 400")

	require.NoError(t, svc.ResetProviderQuota(ctx, parent.ID), "母提供商 reset-quota 应放行")
}

// TestCreateShadow_RejectsShadowAsParent 验证不允许把影子当母创建二级影子。
func TestCreateShadow_RejectsShadowAsParent(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)

	parent := &providercore.Record{
		Name: "real-parent", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
		Status: billing.StatusActive, Credentials: map[string]any{"chatgpt_account_id": "org-x"},
	}
	require.NoError(t, repo.Create(ctx, parent))
	firstShadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "first-shadow"})
	require.NoError(t, err)

	_, err = svc.CreateShadow(ctx, firstShadow.ID, providercore.ShadowOptions{Name: "second-shadow"})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err), "影子当母应返回 400")
}

// TestCreateShadow_StructuredErrors 检查业务错误返回结构化 4xx 响应。
func TestCreateShadow_StructuredErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("non_oauth_parent_400", func(t *testing.T) {
		repo := newSparkShadowRepoStub()
		svc := newProviderEditorForTest(repo)
		parent := &providercore.Record{Name: "apikey-parent", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeAPIKey, Status: billing.StatusActive}
		require.NoError(t, repo.Create(ctx, parent))
		_, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "s"})
		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err), "非 OAuth 母提供商应 400")
	})

	t.Run("duplicate_409", func(t *testing.T) {
		repo := newSparkShadowRepoStub()
		svc := newProviderEditorForTest(repo)
		parent := &providercore.Record{Name: "p", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth, Status: billing.StatusActive, Credentials: map[string]any{"chatgpt_account_id": "o"}}
		require.NoError(t, repo.Create(ctx, parent))
		_, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "s1"})
		require.NoError(t, err)
		_, err = svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "s2"})
		require.Error(t, err)
		require.Equal(t, http.StatusConflict, httpx.ErrorCode(err), "重复创建应 409")
	})
}

// TestUpdateProvider_RejectsTypeChangeOnShadow 验证影子 type 不可被普通更新改坏。
func TestUpdateProvider_RejectsTypeChangeOnShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)
	parent := &providercore.Record{
		Name: "type-parent", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
		Status: billing.StatusActive, Credentials: map[string]any{"chatgpt_account_id": "org-t"},
	}
	require.NoError(t, repo.Create(ctx, parent))
	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "type-shadow"})
	require.NoError(t, err)

	_, err = svc.UpdateProvider(ctx, shadow.ID, &providercore.UpdateProviderInput{Type: capability.ProviderTypeAPIKey})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err), "改影子 type 应 400")
	require.Equal(t, capability.ProviderTypeOAuth, repo.providers[shadow.ID].Type, "影子 type 必须保持 oauth")

	_, err = svc.UpdateProvider(ctx, shadow.ID, &providercore.UpdateProviderInput{Type: capability.ProviderTypeOAuth})
	require.NoError(t, err, "传入相同 type 应允许")
}

// TestBulkUpdateProviders_RejectsCredentialWriteToShadow 验证批量更新携带凭据时
// 目标含影子必须被拒(与单提供商 UpdateProvider 守卫对齐,堵住 bulk 绕过)。
func TestBulkUpdateProviders_RejectsCredentialWriteToShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)
	parent := &providercore.Record{
		Name: "bulk-parent", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
		Status: billing.StatusActive, Credentials: map[string]any{"chatgpt_account_id": "org-b", "access_token": "t"},
	}
	require.NoError(t, repo.Create(ctx, parent))
	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "bulk-shadow"})
	require.NoError(t, err)

	_, err = svc.BulkUpdateProviders(ctx, &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{shadow.ID},
		Credentials: map[string]any{"access_token": "leaked"},
	})
	require.Error(t, err, "批量给影子写凭据必须被拒")
	require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err), "应 400")

	require.Empty(t, repo.providers[shadow.ID].GetOpenAIAccessToken(), "影子 access_token 必须保持为空 —— 批量写入未生效")
}

func TestDeleteProvider_CascadeToShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)

	parent := &providercore.Record{
		Name:        "cascade-parent",
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Credentials: map[string]any{"chatgpt_account_id": "org-cascade"},
	}
	require.NoError(t, repo.Create(ctx, parent))

	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "cascade-shadow"})
	require.NoError(t, err)
	shadowID := shadow.ID

	_, ok := repo.providers[parent.ID]
	require.True(t, ok)
	_, ok = repo.providers[shadowID]
	require.True(t, ok)

	require.NoError(t, svc.DeleteProvider(ctx, parent.ID))

	_, ok = repo.providers[parent.ID]
	require.False(t, ok, "parent provider should be deleted")

	_, ok = repo.providers[shadowID]
	require.False(t, ok, "shadow provider should be cascade-deleted")
}

// TestUpdateProvider_PropagatesProxyToShadow verifies that updating a parent
// provider's ProxyID propagates the new value to its spark shadow.
func TestUpdateProvider_PropagatesProxyToShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)

	oldProxy := int64(7)
	parent := &providercore.Record{
		Name:        "proxy-parent",
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		ProxyID:     &oldProxy,
		Credentials: map[string]any{"chatgpt_account_id": "org-proxy"},
	}
	require.NoError(t, repo.Create(ctx, parent))

	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "proxy-shadow"})
	require.NoError(t, err)
	shadowID := shadow.ID

	newProxy := int64(42)
	_, err = svc.UpdateProvider(ctx, parent.ID, &providercore.UpdateProviderInput{ProxyID: &newProxy})
	require.NoError(t, err)

	storedShadow, ok := repo.providers[shadowID]
	require.True(t, ok)
	require.NotNil(t, storedShadow.ProxyID)
	require.Equal(t, newProxy, *storedShadow.ProxyID)
}

// TestUpdateProvider_RejectsCredentialWriteToShadow 检查影子提供商拒绝写入鉴权凭据。
// 在通用更新路径(UpdateProvider,被 edit/re-auth/refresh/batch 共用)上也被守住:
// 对影子写入 access_token/refresh_token 必须被拒绝,且影子的 access_token/refresh_token
// 保持为空(Credentials 本身允许持有 CreateShadow 写入的 model_mapping,故不能断言整体为空)。
func TestUpdateProvider_RejectsCredentialWriteToShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)

	parent := &providercore.Record{
		Name:        "cred-parent",
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		Credentials: map[string]any{"access_token": "parent-secret", "refresh_token": "parent-rt"},
	}
	require.NoError(t, repo.Create(ctx, parent))

	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "cred-shadow"})
	require.NoError(t, err)
	require.Empty(t, shadow.GetOpenAIAccessToken(), "前提:影子创建后不持有 access_token")
	require.Empty(t, shadow.GetOpenAIRefreshToken(), "前提:影子创建后不持有 refresh_token")

	_, err = svc.UpdateProvider(ctx, shadow.ID, &providercore.UpdateProviderInput{
		Credentials: map[string]any{"access_token": "leaked", "refresh_token": "leaked-rt"},
	})
	require.Error(t, err, "对影子写入凭据必须被拒绝")

	require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err), "应映射为 400 而非 500")

	storedShadow, ok := repo.providers[shadow.ID]
	require.True(t, ok)
	require.Empty(t, storedShadow.GetOpenAIAccessToken(), "影子 access_token 必须保持为空 —— 凭据未被写入")
	require.Empty(t, storedShadow.GetOpenAIRefreshToken(), "影子 refresh_token 必须保持为空 —— 凭据未被写入")

	newPriority := 5
	_, err = svc.UpdateProvider(ctx, shadow.ID, &providercore.UpdateProviderInput{Priority: &newPriority})
	require.NoError(t, err, "影子的非凭据字段更新应正常")
}

// TestBulkUpdateProviders_PropagatesProxyToShadow verifies that bulk-updating
// providers' ProxyID propagates the new value to each provider's spark shadow.
func TestBulkUpdateProviders_PropagatesProxyToShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)

	oldProxy := int64(7)
	parent := &providercore.Record{
		Name:        "bulk-parent",
		Platform:    capability.PlatformOpenAI,
		Type:        capability.ProviderTypeOAuth,
		Status:      billing.StatusActive,
		ProxyID:     &oldProxy,
		Credentials: map[string]any{"chatgpt_account_id": "org-bulk"},
	}
	require.NoError(t, repo.Create(ctx, parent))

	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "bulk-shadow"})
	require.NoError(t, err)
	shadowID := shadow.ID

	newProxy := int64(99)
	_, err = svc.BulkUpdateProviders(ctx, &providercore.BulkUpdateProvidersInput{
		ProviderIDs: []int64{parent.ID},
		ProxyID:     &newProxy,
	})
	require.NoError(t, err)

	storedShadow, ok := repo.providers[shadowID]
	require.True(t, ok)
	require.NotNil(t, storedShadow.ProxyID)
	require.Equal(t, newProxy, *storedShadow.ProxyID)
}

// raceCreateRepoStub 模拟并发竞态:对影子的 Create 撞一母一影唯一索引(返回错误),
// 且复查时另一并发请求的影子已存在 → CreateShadow 应映射为结构化 409。
type raceCreateRepoStub struct {
	*sparkShadowRepoStub
}

func (s *raceCreateRepoStub) Create(ctx context.Context, provider *providercore.Record) error {
	if provider.ParentProviderID != nil {

		s.nextID++
		phantom := *provider
		phantom.ID = s.nextID
		s.providers[phantom.ID] = &phantom
		return errors.New(`duplicate key value violates unique constraint "uq_providers_spark_shadow_per_parent"`)
	}
	return s.sparkShadowRepoStub.Create(ctx, provider)
}

// TestCreateShadow_DefaultsNameFromParent 验证空 name 不应 500,
// 而是默认 "<母提供商名> (Spark)"。
func TestCreateShadow_DefaultsNameFromParent(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)
	parent := &providercore.Record{
		Name: "mum", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
		Status: billing.StatusActive, Credentials: map[string]any{"chatgpt_account_id": "o"},
	}
	require.NoError(t, repo.Create(ctx, parent))

	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "   "})
	require.NoError(t, err, "空/空白 name 不应 500,应默认命名")
	require.Equal(t, "mum (Spark)", shadow.Name)
}

// TestCreateShadow_ConcurrentCreateReturns409 验证并发竞态下预查放行后
// Create 遇到唯一索引冲突时返回结构化 409 响应。
func TestCreateShadow_ConcurrentCreateReturns409(t *testing.T) {
	ctx := context.Background()
	base := newSparkShadowRepoStub()
	repo := &raceCreateRepoStub{sparkShadowRepoStub: base}
	svc := newProviderEditorForTest(repo)
	parent := &providercore.Record{
		Name: "p", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
		Status: billing.StatusActive, Credentials: map[string]any{"chatgpt_account_id": "o"},
	}
	require.NoError(t, base.Create(ctx, parent))

	_, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "s"})
	require.Error(t, err)
	require.Equal(t, http.StatusConflict, httpx.ErrorCode(err), "并发竞态撞唯一索引应映射 409 而非 500")
}

// TestUpdateProvider_RejectsParentTypeChangeWithShadow 验证母提供商有 spark 影子时,
// 不能把 type 改出 OpenAI OAuth(否则影子被调度后透传凭据解析必失败)。
func TestUpdateProvider_RejectsParentTypeChangeWithShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)
	parent := &providercore.Record{
		Name: "p", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
		Status: billing.StatusActive, Credentials: map[string]any{"chatgpt_account_id": "o"},
	}
	require.NoError(t, repo.Create(ctx, parent))
	_, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "s"})
	require.NoError(t, err)

	_, err = svc.UpdateProvider(ctx, parent.ID, &providercore.UpdateProviderInput{Type: capability.ProviderTypeAPIKey})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err), "母提供商有影子时改 type 出 oauth 应 400")
	require.Equal(t, capability.ProviderTypeOAuth, repo.providers[parent.ID].Type, "母提供商 type 必须保持 oauth")

	_, err = svc.UpdateProvider(ctx, parent.ID, &providercore.UpdateProviderInput{Type: capability.ProviderTypeOAuth})
	require.NoError(t, err, "传入相同 type(no-op)应允许")
}

// TestUpdateProvider_IgnoresProxyChangeOnShadow 验证影子 proxy 恒继承母提供商,
// 普通更新不得独立改动。
func TestUpdateProvider_IgnoresProxyChangeOnShadow(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)
	parentProxy := int64(7)
	parent := &providercore.Record{
		Name: "p", Platform: capability.PlatformOpenAI, Type: capability.ProviderTypeOAuth,
		Status: billing.StatusActive, ProxyID: &parentProxy,
		Credentials: map[string]any{"chatgpt_account_id": "o"},
	}
	require.NoError(t, repo.Create(ctx, parent))
	shadow, err := svc.CreateShadow(ctx, parent.ID, providercore.ShadowOptions{Name: "s"})
	require.NoError(t, err)
	require.NotNil(t, repo.providers[shadow.ID].ProxyID)
	require.Equal(t, parentProxy, *repo.providers[shadow.ID].ProxyID, "前提:影子继承母 proxy=7")

	newProxy := int64(42)
	_, err = svc.UpdateProvider(ctx, shadow.ID, &providercore.UpdateProviderInput{ProxyID: &newProxy})
	require.NoError(t, err, "影子的非 proxy 字段更新仍应成功")
	require.NotNil(t, repo.providers[shadow.ID].ProxyID)
	require.Equal(t, parentProxy, *repo.providers[shadow.ID].ProxyID, "影子 proxy 不应被独立改动,恒继承母提供商")
}

func TestUpdateProvider_ShadowEmptyCredentialsClearsModelMapping(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)
	parentID := int64(1)
	shadow := &providercore.Record{
		Name:             "s",
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		Status:           billing.StatusActive,
		ParentProviderID: &parentID,
		QuotaDimension:   providercore.QuotaDimensionSpark,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gpt-5.3-codex-spark": "gpt-5.3-codex-spark",
			},
		},
	}
	require.NoError(t, repo.Create(ctx, shadow))

	updated, err := svc.UpdateProvider(ctx, shadow.ID, &providercore.UpdateProviderInput{
		Credentials: map[string]any{},
	})

	require.NoError(t, err)
	require.Empty(t, updated.Credentials)
	require.Empty(t, repo.providers[shadow.ID].Credentials)
}

func TestUpdateProvider_ShadowRejectsAuthCredentials(t *testing.T) {
	ctx := context.Background()
	repo := newSparkShadowRepoStub()
	svc := newProviderEditorForTest(repo)
	parentID := int64(1)
	shadow := &providercore.Record{
		Name:             "s",
		Platform:         capability.PlatformOpenAI,
		Type:             capability.ProviderTypeOAuth,
		Status:           billing.StatusActive,
		ParentProviderID: &parentID,
		QuotaDimension:   providercore.QuotaDimensionSpark,
		Credentials:      map[string]any{},
	}
	require.NoError(t, repo.Create(ctx, shadow))

	_, err := svc.UpdateProvider(ctx, shadow.ID, &providercore.UpdateProviderInput{
		Credentials: map[string]any{"access_token": "leak"},
	})

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, httpx.ErrorCode(err))
	require.Empty(t, repo.providers[shadow.ID].Credentials)
}

// GetByIDs 按请求顺序返回存在的提供商。
func (s *sparkShadowRepoStub) GetByIDs(_ context.Context, ids []int64) ([]*providercore.Record, error) {
	var out []*providercore.Record
	for _, id := range ids {
		if value, ok := s.providersByID[id]; ok {
			out = append(out, providercore.CloneRecord(value))
		}
	}
	return out, nil
}

// BulkUpdate 替身返回零行，影子同步由管理用例执行。
func (*sparkShadowRepoStub) BulkUpdate(context.Context, []int64, providercore.ProviderBulkUpdate) (int64, error) {
	return 0, nil
}

func (*sparkShadowRepoStub) ResetQuotaUsedAndClearRateLimitCooldown(context.Context, int64) error {
	return nil
}
