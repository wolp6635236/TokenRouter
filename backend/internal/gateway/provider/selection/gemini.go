package selection

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	schedulercore "github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
)

const geminiStickySessionTTL = time.Hour

func (s *Gemini) SelectProviderForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*gatewayprovider.ExecutionProvider, error) {
	ctx = withSelectionRequest(ctx, groupID, requestedModel)
	core, scope := s.geminiSelector()
	selected, err := core.SelectOnly(ctx, schedulercore.SelectionInput{
		GroupID: groupID, SessionHash: sessionHash, RequestedModel: requestedModel,
		ExcludedIDs: excludedIDs,
	})
	return scope.oldProvider(selected), err
}

// resolvePlatformAndSchedulingMode 解析目标平台和调度模式。
// 返回：平台名称、是否使用混合调度、是否强制平台、已解析分组、错误。
func (s *Gemini) resolvePlatformAndSchedulingMode(ctx context.Context, groupID *int64) (platform string, useMixedScheduling bool, hasForcePlatform bool, group *routing.Group, err error) {
	var read func(context.Context, int64) (*routing.Group, error)
	if s.groupRepo != nil {
		read = s.groupRepo.GetByIDLite
	}
	group, err = currentSelectionGroup(ctx, groupID, read)
	if err != nil {
		return "", false, false, nil, err
	}
	platform, forced := apikey.ForcePlatformFromContext(ctx)
	if forced && platform != "" {
		return platform, false, true, group, nil
	}
	return "", false, false, group, nil
}

// tryStickySessionHit 尝试从粘性会话获取提供商。
// 如果命中且提供商可用则返回提供商；如果提供商不可用则清理会话并返回 nil。
//
// tryStickySessionHit attempts to get provider from sticky session.
// Returns provider if hit and usable; clears session and returns nil if provider unavailable.
func (s *Gemini) tryStickySessionHit(
	ctx context.Context,
	groupID *int64,
	sessionHash, cacheKey, requestedModel string,
	excludedIDs map[int64]struct{},
	platform string,
	useMixedScheduling bool,
) *gatewayprovider.ExecutionProvider {
	if sessionHash == "" {
		return nil
	}

	providerID, err := s.cache.GetSessionProviderID(ctx, derefGroupID(groupID), cacheKey)
	if err != nil || providerID <= 0 {
		return nil
	}

	if _, excluded := excludedIDs[providerID]; excluded {
		return nil
	}

	provider, err := s.getSchedulableProvider(ctx, providerID)
	if err != nil {
		return nil
	}

	if shouldClearStickySession(provider, requestedModel) {
		_ = s.cache.DeleteSessionProviderID(ctx, derefGroupID(groupID), cacheKey)
		return nil
	}

	if !openAIStickyProviderMatchesGroup(provider, groupID) || !s.isProviderUsableForRequest(ctx, provider, requestedModel, platform, useMixedScheduling) {
		return nil
	}

	_ = s.cache.RefreshSessionTTL(ctx, derefGroupID(groupID), cacheKey, geminiStickySessionTTL)
	return provider
}

// isProviderUsableForRequest 检查提供商是否可用于当前请求。
// 验证：模型调度、模型支持、平台匹配、速率限制预检。
//
// isProviderUsableForRequest checks if provider is usable for current request.
// Validates: model scheduling, model support, platform matching, rate limit precheck.
func (s *Gemini) isProviderUsableForRequest(
	ctx context.Context,
	provider *gatewayprovider.ExecutionProvider,
	requestedModel, platform string,
	useMixedScheduling bool,
) bool {
	return s.isProviderUsableForRequestWithPrecheck(ctx, provider, requestedModel, platform, useMixedScheduling, nil)
}

func (s *Gemini) isProviderUsableForRequestWithPrecheck(
	ctx context.Context,
	provider *gatewayprovider.ExecutionProvider,
	requestedModel, platform string,
	useMixedScheduling bool,
	precheckResult map[int64]bool,
) bool {
	if !gatewayprovider.ExecutionModelPolicy(provider).Schedulable(ctx, requestedModel) {
		return false
	}

	if requestedModel != "" && !s.isModelSupportedByProvider(provider, requestedModel) {
		return false
	}

	if !s.isProviderValidForPlatform(provider, platform, useMixedScheduling) {
		return false
	}

	if !s.passesRateLimitPreCheckWithCache(ctx, provider, requestedModel, precheckResult) {
		return false
	}

	return true
}

// isProviderValidForPlatform 检查提供商是否匹配目标平台。
// 普通请求使用分组的全部候选；强制平台请求额外限定提供商平台。
//
// isProviderValidForPlatform checks if provider matches target platform.
func (s *Gemini) isProviderValidForPlatform(provider *gatewayprovider.ExecutionProvider, platform string, useMixedScheduling bool) bool {
	return provider != nil && (platform == "" || provider.Record.Platform == platform)
}

func (s *Gemini) passesRateLimitPreCheckWithCache(ctx context.Context, provider *gatewayprovider.ExecutionProvider, requestedModel string, precheckResult map[int64]bool) bool {
	if s.quotaPrecheck == nil || requestedModel == "" {
		return true
	}

	if precheckResult != nil {
		if ok, exists := precheckResult[provider.Record.ID]; exists {
			return ok
		}
	}

	ok, err := s.quotaPrecheck.PreCheckUsage(ctx, gatewayprovider.ExecutionRecord(provider), requestedModel)
	if err != nil {
		logging.LegacyPrintf("service.gemini_messages_compat", "[Gemini PreCheck] Provider %d precheck error: %v", provider.Record.ID, err)
	}
	return ok
}

// eligibleGeminiProviders 在高级和基础调度器前复用 Gemini 现有的全部硬过滤规则。
func (s *Gemini) eligibleGeminiProviders(
	ctx context.Context,
	providers []gatewayprovider.ExecutionProvider,
	requestedModel string,
	excludedIDs map[int64]struct{},
	platform string,
	useMixedScheduling bool,
) []*gatewayprovider.ExecutionProvider {
	precheckResult := s.buildPreCheckUsageResultMap(ctx, providers, requestedModel)
	eligible := make([]*gatewayprovider.ExecutionProvider, 0, len(providers))

	for i := range providers {
		acc := &providers[i]

		if _, excluded := excludedIDs[acc.Record.ID]; excluded {
			continue
		}

		if !s.isProviderUsableForRequestWithPrecheck(ctx, acc, requestedModel, platform, useMixedScheduling, precheckResult) {
			continue
		}
		eligible = append(eligible, acc)
	}

	return eligible
}

// groupUsesAdvancedScheduler 根据最终分组配置判断是否使用高级调度，无分组时返回 false。
func (s *Gemini) groupUsesAdvancedScheduler(ctx context.Context, groupID *int64, hasForcePlatform bool) bool {
	if s == nil || groupID == nil || *groupID <= 0 {
		return false
	}
	if group, ok := requeststate.GroupFromContext(ctx); ok && routing.IsGroupContextValid(group) && group.ID == *groupID {
		return group.UsesAdvancedScheduler()
	}
	if s.schedulerSnapshot != nil {
		if group, err := s.readSchedulingGroup(ctx, *groupID); err == nil && group != nil {
			return group.UsesAdvancedScheduler()
		}
	}
	if s.groupRepo == nil {
		return false
	}
	group, err := s.groupRepo.GetByIDLite(ctx, *groupID)
	return err == nil && group != nil && group.UsesAdvancedScheduler()
}

func (s *Gemini) advancedSchedulerStats() *schedulercore.RuntimeStats {
	if s == nil {
		return nil
	}
	if s.advancedProviderStats == nil {
		s.advancedProviderStats = schedulercore.NewRuntimeStats(time.Now)
	}
	return s.advancedProviderStats
}

// advancedSchedulerEffectiveSettingsForRequest 返回最终分组的高级调度有效配置。
func (s *Gemini) advancedSchedulerEffectiveSettingsForRequest(ctx context.Context, id *int64) policy.EffectiveSettings {
	if ctx == nil {
		ctx = context.Background()
	}
	group := schedulerRequestGroup(ctx, id, s.schedulerSnapshot != nil, s.readSchedulingGroup)
	return s.schedulerParameters.Effective(ctx, schedulerGroupOverrides(group))
}

// selectAdvancedGeminiProvider 在 Gemini 已完成硬过滤后复用通用高级评分与 Top-K 选择。
func (s *Gemini) selectAdvancedGeminiProvider(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	cacheKey string,
	eligible []*gatewayprovider.ExecutionProvider,
	settings policy.EffectiveSettings,
) *gatewayprovider.ExecutionProvider {
	if len(eligible) == 0 {
		return nil
	}
	var stickyProviderID int64
	if sessionHash != "" && s.cache != nil {
		stickyProviderID, _ = s.cache.GetSessionProviderID(ctx, derefGroupID(groupID), cacheKey)
	}
	input := schedulercore.ScoreInput{
		GroupID:          groupID,
		SessionHash:      cacheKey,
		StickyProviderID: stickyProviderID,
		StickyWeighted:   settings.StickyWeightedEnabled,
		TopK:             settings.TopK,
	}
	values := make([]*schedulercore.ScoreProvider, len(eligible))
	source := make(map[*schedulercore.ScoreProvider]*gatewayprovider.ExecutionProvider, len(eligible))
	for i, value := range eligible {
		if value == nil {
			continue
		}
		values[i] = &schedulercore.ScoreProvider{ID: value.Record.ID, Platform: value.Record.Platform, Priority: value.Record.Priority, SessionWindowEnd: value.Record.SessionWindowEnd}
		source[values[i]] = value
	}
	candidates, _ := schedulercore.ScoreCandidates(values, nil, s.advancedSchedulerStats(), settings.Weights, input, time.Now())
	selectionOrder := schedulercore.BuildSelectionOrder(candidates, input)
	if len(selectionOrder) == 0 {
		return nil
	}
	return source[selectionOrder[0].Provider]
}

// groupModelUnsupportedErrorIfApplicable 在确认是分组模型限制时返回 typed error。
func (s *Gemini) groupModelUnsupportedErrorIfApplicable(ctx context.Context, providers []gatewayprovider.ExecutionProvider, requestedModel string, platform string, excludedIDs map[int64]struct{}, useMixedScheduling bool) error {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" || len(providers) == 0 {
		return nil
	}
	precheckResult := s.buildPreCheckUsageResultMap(ctx, providers, requestedModel)
	hasRelevantProvider := false
	for i := range providers {
		acc := &providers[i]
		if excludedIDs != nil {
			if _, excluded := excludedIDs[acc.Record.ID]; excluded {
				continue
			}
		}
		if !acc.View().IsSchedulable() || !s.isProviderValidForPlatform(acc, platform, useMixedScheduling) || !s.passesRateLimitPreCheckWithCache(ctx, acc, requestedModel, precheckResult) {
			continue
		}
		hasRelevantProvider = true
		if s.isModelSupportedByProvider(acc, requestedModel) {
			return nil
		}
	}
	if !hasRelevantProvider {
		return nil
	}
	return routing.NewGroupModelRejection(platform, requestedModel, modelRejectionSources(providers))
}

func (s *Gemini) buildPreCheckUsageResultMap(ctx context.Context, providers []gatewayprovider.ExecutionProvider, requestedModel string) map[int64]bool {
	if s.quotaPrecheck == nil || requestedModel == "" || len(providers) == 0 {
		return nil
	}

	candidates := make([]*gatewayprovider.ExecutionProvider, 0, len(providers))
	for i := range providers {
		candidates = append(candidates, &providers[i])
	}

	result, err := s.quotaPrecheck.PreCheckUsageBatch(ctx, gatewayprovider.ExecutionRecordPointers(candidates), requestedModel)
	if err != nil {
		logging.LegacyPrintf("service.gemini_messages_compat", "[Gemini PreCheckBatch] failed: %v", err)
	}
	return result
}

// isModelSupportedByProvider 根据提供商平台检查模型支持。
func (s *Gemini) isModelSupportedByProvider(provider *gatewayprovider.ExecutionProvider, requestedModel string) bool {
	if provider.Record.Platform == capability.PlatformAntigravity {
		if strings.TrimSpace(requestedModel) == "" {
			return true
		}
		return mapAntigravityModel(provider, requestedModel) != ""
	}
	return gatewayprovider.ExecutionProtocolRecord(provider).IsModelSupported(requestedModel, provideradapter.ModelDefaults(), provideradapter.ModelRules(gatewayprovider.ExecutionProtocolRecord(provider)))
}

func (s *Gemini) getSchedulableProvider(ctx context.Context, providerID int64) (*gatewayprovider.ExecutionProvider, error) {
	if s.schedulerSnapshot != nil {
		return readSnapshotProvider(ctx, s.schedulerSnapshot, providerID)
	}
	return s.providerRepo.GetByID(ctx, providerID)
}

func (s *Gemini) hydrateSelectedProvider(ctx context.Context, provider *gatewayprovider.ExecutionProvider) (*gatewayprovider.ExecutionProvider, error) {
	if provider == nil || s.schedulerSnapshot == nil {
		return provider, nil
	}
	var hydrated *gatewayprovider.ExecutionProvider
	var err error
	if s.providerRepo != nil {
		hydrated, err = s.providerRepo.GetByID(ctx, provider.Record.ID)
	} else {
		hydrated, err = readSnapshotProvider(ctx, s.schedulerSnapshot, provider.Record.ID)
	}
	if err != nil {
		return nil, err
	}
	if hydrated == nil {
		return nil, schedulercore.ErrNoAvailableProviders
	}
	if input, ok := ctx.Value(selectionRequestKey{}).(selectionRequest); ok {
		groupID := input.groupID
		if group, ok := requeststate.GroupFromContext(ctx); ok && group != nil {
			groupID = &group.ID
		}
		model := input.model
		policy := gatewayprovider.ExecutionModelPolicy(hydrated)
		if !openAIStickyProviderMatchesGroup(hydrated, groupID) || !policy.Schedulable(ctx, model) || !policy.Supports(ctx, model) {
			return nil, schedulercore.ErrNoAvailableProviders
		}
	}
	return hydrated, nil
}

func (s *Gemini) listSchedulableProvidersOnce(ctx context.Context, groupID *int64, platform string, hasForcePlatform bool) ([]gatewayprovider.ExecutionProvider, error) {
	if groupID == nil || *groupID <= 0 {
		return nil, nil
	}
	if s.schedulerSnapshot != nil {
		providers, _, err := readSnapshotProviders(ctx, s.schedulerSnapshot, groupID, platform, hasForcePlatform)
		return providers, err
	}
	if platform == "" {
		return s.providerRepo.ListSchedulableByGroupIDAndPlatforms(ctx, *groupID, capability.ProviderPlatforms())
	}
	return s.providerRepo.ListSchedulableByGroupIDAndPlatform(ctx, *groupID, platform)
}

// HasAntigravityProviders 检查是否有可用的 antigravity 提供商。
func (s *Gemini) HasAntigravityProviders(ctx context.Context, groupID *int64) (bool, error) {
	providers, err := s.listSchedulableProvidersOnce(ctx, groupID, capability.PlatformAntigravity, false)
	if err != nil {
		return false, err
	}
	return len(providers) > 0, nil
}

// SelectProviderForAIStudioEndpoints 为 generativelanguage.googleapis.com
// （如 GET /v1beta/models）选择适合的提供商。
// 优先使用 AI Studio API Key，然后依次尝试无 project_id 的 OAuth、
// 将 ai_studio OAuth 与其余 Gemini 提供商分开。
func (s *Gemini) SelectProviderForAIStudioEndpoints(ctx context.Context, groupID *int64) (*gatewayprovider.ExecutionProvider, error) {
	var read func(context.Context, int64) (*routing.Group, error)
	if s.groupRepo != nil {
		read = s.groupRepo.GetByIDLite
	}
	current, groupErr := currentSelectionGroup(ctx, groupID, read)
	if groupErr != nil {
		return nil, groupErr
	}
	if current != nil {
		ctx = requeststate.WithGroup(ctx, current)
	}
	if group, ok := s.resolveAdvancedSchedulerGroup(ctx, groupID); ok {
		ctx = requeststate.WithGroup(ctx, group)
	}
	providers, err := s.listSchedulableProvidersOnce(ctx, groupID, capability.PlatformGemini, true)
	if err != nil {
		return nil, fmt.Errorf("query providers failed: %w", err)
	}
	if len(providers) == 0 {
		return nil, errors.New("no available Gemini providers")
	}

	rank := func(a *gatewayprovider.ExecutionProvider) int {
		if a == nil {
			return 999
		}
		switch a.Record.Type {
		case capability.ProviderTypeAPIKey:
			if strings.TrimSpace(a.View().GetCredential("api_key")) != "" {
				return 0
			}
			return 9
		case capability.ProviderTypeOAuth:
			if strings.TrimSpace(a.View().GetCredential("project_id")) == "" {
				return 1
			}
			if strings.TrimSpace(a.View().GetCredential("oauth_type")) == "ai_studio" {
				return 2
			}

			return 3
		case capability.ProviderTypeServiceAccount:

			return 999
		default:
			return 10
		}
	}

	var selected *gatewayprovider.ExecutionProvider
	for i := range providers {
		acc := &providers[i]
		if selected == nil {
			selected = acc
			continue
		}

		r1, r2 := rank(acc), rank(selected)
		if r1 < r2 {
			selected = acc
			continue
		}
		if r1 > r2 {
			continue
		}

		if acc.Record.Priority < selected.Record.Priority {
			selected = acc
		} else if acc.Record.Priority == selected.Record.Priority {
			switch {
			case acc.Record.LastUsedAt == nil && selected.Record.LastUsedAt != nil:
				selected = acc
			case acc.Record.LastUsedAt != nil && selected.Record.LastUsedAt == nil:

			case acc.Record.LastUsedAt == nil && selected.Record.LastUsedAt == nil:
				if acc.Record.Type == capability.ProviderTypeOAuth && selected.Record.Type != capability.ProviderTypeOAuth {
					selected = acc
				}
			default:
				if acc.Record.LastUsedAt.Before(*selected.Record.LastUsedAt) {
					selected = acc
				}
			}
		}
	}

	if selected == nil {
		return nil, errors.New("no available Gemini providers")
	}

	if s.groupUsesAdvancedScheduler(ctx, groupID, false) {
		bestRank := rank(selected)
		if bestRank < 999 {
			eligible := make([]*gatewayprovider.ExecutionProvider, 0, len(providers))
			for i := range providers {
				provider := &providers[i]
				if rank(provider) == bestRank {
					eligible = append(eligible, provider)
				}
			}
			if advanced := s.selectAdvancedGeminiProvider(
				ctx,
				groupID,
				"",
				"",
				eligible,
				s.advancedSchedulerEffectiveSettingsForRequest(ctx, groupID),
			); advanced != nil {
				selected = advanced
			}
		}
	}
	return s.hydrateSelectedProvider(ctx, selected)
}

// resolveAdvancedSchedulerGroup 为不经过普通模型选择的 Gemini 入口补齐最终分组。
func (s *Gemini) resolveAdvancedSchedulerGroup(ctx context.Context, groupID *int64) (*routing.Group, bool) {
	if s == nil || groupID == nil || *groupID <= 0 {
		return nil, false
	}
	if group, ok := requeststate.GroupFromContext(ctx); ok && routing.IsGroupContextValid(group) && group.ID == *groupID {
		return group, true
	}
	if s.schedulerSnapshot != nil {
		if group, err := s.readSchedulingGroup(ctx, *groupID); err == nil && group != nil {
			return group, true
		}
	}
	if s.groupRepo == nil {
		return nil, false
	}
	group, err := s.groupRepo.GetByIDLite(ctx, *groupID)
	return group, err == nil && group != nil
}

// readSchedulingGroup 从 Gemini 分组仓库读取调度配置。
func (s *Gemini) readSchedulingGroup(ctx context.Context, id int64) (*routing.Group, error) {
	if s.groupRepo == nil {
		return nil, nil
	}
	return s.groupRepo.GetByID(ctx, id)
}
