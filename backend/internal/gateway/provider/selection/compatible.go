package selection

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	schedulercore "github.com/TokenFlux/TokenRouter/internal/scheduler"
)

// openAINoAvailableSelectionError 提供错误消息，并支持 errors.Is 分类。
type openAINoAvailableSelectionError struct {
	message string
}

// BindStickySession 使用调度器默认期限绑定提供商会话。
func (s *Compatible) BindStickySession(ctx context.Context, groupID *int64, sessionHash string, providerID int64) error {
	return s.BindStickySessionWithTTL(ctx, groupID, sessionHash, providerID, s.SessionStickyTTL())
}

// BindStickySessionWithTTL 让 WS 请求按当前参数保存会话期限。
func (s *Compatible) BindStickySessionWithTTL(ctx context.Context, groupID *int64, sessionHash string, providerID int64, ttl time.Duration) error {
	if sessionHash == "" || providerID <= 0 || requeststate.PreserveGuardianParentBinding(ctx, sessionHash) {
		return nil
	}
	return s.setStickySessionProviderID(ctx, groupID, sessionHash, providerID, ttl)
}

// SelectProviderForModel 选择支持请求模型的提供商。
func (s *Compatible) SelectProviderForModel(ctx context.Context, groupID *int64, sessionHash string, requestedModel string) (*gatewayprovider.ExecutionProvider, error) {
	return s.SelectProviderForModelWithExclusions(ctx, groupID, sessionHash, requestedModel, nil)
}

// SelectProviderForModelWithExclusions 选择支持请求模型且不在排除集合中的提供商。
// SelectProviderForModelWithExclusions 选择支持指定模型的提供商，同时排除指定的提供商。
func (s *Compatible) SelectProviderForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*gatewayprovider.ExecutionProvider, error) {
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	resolvedCtx, resolvedGroupID, err := s.resolveOpenAISchedulerGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	ctx = resolvedCtx
	groupID = resolvedGroupID
	if s.groupUsesAdvancedScheduler(ctx, groupID) {
		selection, _, selectErr := s.SelectProviderWithScheduler(
			schedulercore.WithSelectOnly(ctx),
			groupID,
			"",
			sessionHash,
			requestedModel,
			excludedIDs, egress.OpenAIUpstreamTransportAny, false,
		)
		if selectErr != nil {
			return nil, selectErr
		}
		if selection == nil || selection.Provider == nil {
			return nil, schedulercore.ErrNoAvailableProviders
		}
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		return selection.Provider, nil
	}
	return s.selectProviderForModelWithExclusions(ctx, groupID, "", sessionHash, requestedModel, excludedIDs, false, 0, "")
}

func shouldUseGroupModelUnsupportedError(ctx context.Context, providers []gatewayprovider.ExecutionProvider, requestedModel string) bool {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" || len(providers) == 0 {
		return false
	}
	hasRelevantProvider := false
	for i := range providers {
		acc := &providers[i]
		if !acc.View().IsSchedulable() {
			continue
		}
		hasRelevantProvider = true
		if gatewayprovider.ExecutionModelPolicy(acc).SupportsCompatibleRouting(ctx, requestedModel) {
			return false
		}
	}
	return hasRelevantProvider
}

// SelectProviderForTokenCount 为不计费的 token 计数请求选择提供商。
// 仍检查平台、模型、能力和运行资格，但不获取或等待生成槽位。
func (s *Compatible) SelectProviderForTokenCount(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	requestedModel string,
	requiredCapability providercore.OpenAIEndpointCapability,
	platform string,
) (*gatewayprovider.ExecutionProvider, error) {
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	return s.selectProviderForModelWithExclusions(
		ctx,
		groupID,
		platform,
		sessionHash,
		requestedModel,
		nil,
		false,
		0,
		requiredCapability,
	)
}

// noAvailableOpenAISelectionErrorForRoutingWithDetails 仅在通用无提供商错误中追加调度诊断；
// compact 不可用时返回能力错误，模型受分组限制时返回模型拒绝错误。
func noAvailableOpenAISelectionErrorForRoutingWithDetails(ctx context.Context, requestedModel string, routingModel string, compactBlocked bool, details string, providers ...[]gatewayprovider.ExecutionProvider) error {
	if compactBlocked {
		return schedulercore.ErrNoAvailableCompactProviders
	}
	if len(providers) > 0 && shouldUseGroupModelUnsupportedError(ctx, providers[0], routingModel) {
		if err := routing.NewGroupModelRejection("", requestedModel, modelRejectionSources(providers[0])); err != nil {
			return err
		}
	}
	message := "no available OpenAI providers"
	if requestedModel != "" {
		message = fmt.Sprintf("no available OpenAI providers supporting model: %s", requestedModel)
	}
	if details != "" {
		message += " (" + details + ")"
	}
	return openAINoAvailableSelectionError{message: message}
}

func (e openAINoAvailableSelectionError) Error() string {
	return e.message
}

func (e openAINoAvailableSelectionError) Unwrap() error {
	return schedulercore.ErrNoAvailableProviders
}

func (s *Compatible) withOpenAIQuotaAutoPauseContext(ctx context.Context) context.Context {
	if s == nil || s.quotaSettings == nil {
		return ctx
	}
	return gatewayprovider.WithQuotaAutoPauseSettings(ctx, s.quotaSettings.GetOpenAIQuotaAutoPauseSettings(ctx))
}

func (s *Compatible) selectProviderForModelWithExclusions(ctx context.Context, groupID *int64, platform string, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, requireCompact bool, stickyProviderID int64, requiredCapability providercore.OpenAIEndpointCapability) (*gatewayprovider.ExecutionProvider, error) {
	routingModel := s.resolveGroupRoutingModel(ctx, groupID, requestedModel)
	return s.selectProviderForModelWithExclusionsForRouting(ctx, groupID, platform, sessionHash, requestedModel, routingModel, excludedIDs, requireCompact, stickyProviderID, requiredCapability)
}

// selectProviderForModelWithExclusionsForRouting 使用已解析的提供商层模型执行基础调度。
func (s *Compatible) selectProviderForModelWithExclusionsForRouting(ctx context.Context, groupID *int64, platform string, sessionHash string, requestedModel string, routingModel string, excludedIDs map[int64]struct{}, requireCompact bool, stickyProviderID int64, requiredCapability providercore.OpenAIEndpointCapability) (*gatewayprovider.ExecutionProvider, error) {
	resolvedCtx, _, groupErr := s.resolveOpenAISchedulerGroup(ctx, groupID)
	if groupErr != nil {
		return nil, groupErr
	}
	ctx = resolvedCtx
	if forced, ok := apikey.ForcePlatformFromContext(ctx); ok && strings.TrimSpace(forced) != "" {
		platform = forced
	}
	ctx = s.withCandidatePolicy(ctx, groupID, sessionHash)
	core, scope := (&compatiblePicker{service: s}).platformSelector()
	selected, err := core.SelectBasicOnly(ctx, schedulercore.PlatformSelectionInput{GroupID: groupID, Platform: platform, SessionHash: sessionHash, RequestedModel: requestedModel, RoutingModel: routingModel, ExcludedIDs: excludedIDs, RequireCompact: requireCompact, StickyProviderID: stickyProviderID, RequiredCapability: requiredCapability})
	return scope.oldProvider(selected), err
}

// SelectProviderWithLoadAwareness 按负载选择提供商，并返回等待方案。
func (s *Compatible) SelectProviderWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*gatewayprovider.SelectionResult, error) {
	return s.selectProviderWithLoadAwareness(s.withOpenAIQuotaAutoPauseContext(ctx), groupID, "", sessionHash, requestedModel, excludedIDs, false, "")
}

func (s *Compatible) selectProviderWithLoadAwareness(ctx context.Context, groupID *int64, platform string, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, requireCompact bool, requiredCapability providercore.OpenAIEndpointCapability) (*gatewayprovider.SelectionResult, error) {
	routingModel := s.resolveGroupRoutingModel(ctx, groupID, requestedModel)
	return s.selectProviderWithLoadAwarenessForRouting(ctx, groupID, platform, sessionHash, requestedModel, routingModel, excludedIDs, requireCompact, requiredCapability)
}

// selectProviderWithLoadAwarenessForRouting 使用已解析的提供商层模型执行负载感知调度。
func (s *Compatible) selectProviderWithLoadAwarenessForRouting(ctx context.Context, groupID *int64, platform string, sessionHash string, requestedModel string, routingModel string, excludedIDs map[int64]struct{}, requireCompact bool, requiredCapability providercore.OpenAIEndpointCapability) (*gatewayprovider.SelectionResult, error) {
	resolvedCtx, _, groupErr := s.resolveOpenAISchedulerGroup(ctx, groupID)
	if groupErr != nil {
		return nil, groupErr
	}
	ctx = resolvedCtx
	if forced, ok := apikey.ForcePlatformFromContext(ctx); ok && strings.TrimSpace(forced) != "" {
		platform = forced
	}
	ctx = s.withCandidatePolicy(ctx, groupID, sessionHash)
	core, scope := (&compatiblePicker{service: s}).platformSelector()
	selected, err := core.SelectBasic(ctx, schedulercore.PlatformSelectionInput{GroupID: groupID, Platform: platform, SessionHash: sessionHash, RequestedModel: requestedModel, RoutingModel: routingModel, ExcludedIDs: excludedIDs, RequireCompact: requireCompact, RequiredCapability: requiredCapability})
	return scope.restore(selected), err
}

func (s *Compatible) listSchedulableProviders(ctx context.Context, groupID *int64, platform string) ([]gatewayprovider.ExecutionProvider, error) {
	if groupID == nil || *groupID <= 0 {
		return nil, nil
	}
	platform = strings.TrimSpace(platform)
	var providers []gatewayprovider.ExecutionProvider
	var err error
	if s.schedulerSnapshot != nil {
		providers, _, err = readSnapshotProviders(ctx, s.schedulerSnapshot, groupID, platform, platform != "")
	} else if platform == "" {
		providers, err = s.providerRepo.ListSchedulableByGroupIDAndPlatforms(ctx, *groupID, capability.ProviderPlatforms())
	} else {
		providers, err = s.providerRepo.ListSchedulableByGroupIDAndPlatform(ctx, *groupID, platform)
	}
	if err != nil {
		return nil, fmt.Errorf("query providers failed: %w", err)
	}
	providers = s.filterOpenAIProvidersBySchedulingThreshold(ctx, providers)
	return s.filterGrokFreeQuotaProvidersForOpenAI(ctx, providers), nil
}

func (s *Compatible) tryAcquireProviderSlot(ctx context.Context, providerID int64, maxConcurrency int) (*schedulercore.AcquireResult, error) {
	if schedulercore.IsSelectOnly(ctx) {
		return &schedulercore.AcquireResult{Acquired: true, ReleaseFunc: func() {}}, nil
	}
	if s.concurrencyService == nil {
		return &schedulercore.AcquireResult{Acquired: true, ReleaseFunc: func() {}}, nil
	}
	return s.concurrencyService.AcquireProviderSlot(ctx, providerID, maxConcurrency)
}

func (s *Compatible) resolveFreshSchedulableOpenAIProvider(ctx context.Context, provider *gatewayprovider.ExecutionProvider, platform string, requestedModel string, requireCompact bool, requiredCapability providercore.OpenAIEndpointCapability) *gatewayprovider.ExecutionProvider {
	if provider == nil {
		return nil
	}
	platform = strings.TrimSpace(platform)

	fresh := provider
	if s.schedulerSnapshot != nil {
		current, err := s.getSchedulableProvider(ctx, provider.Record.ID)
		if err != nil || current == nil {
			return nil
		}
		fresh = current
	}

	if s.candidateEligibilityReason(ctx, fresh, platform, requestedModel, requireCompact, requiredCapability) != "" {
		return nil
	}
	if !s.shadowProtocolsAllowed(ctx, fresh) || !providercore.ParentHealthyForShadow(gatewayprovider.ExecutionRecord(fresh), func(id int64) *providercore.Record {
		return gatewayprovider.ExecutionRecord(s.parentProviderLookup(ctx)(id))
	}) {
		return nil
	}
	if s.isOpenAIProviderRequestRuntimeBlocked(fresh, requestedModel) {
		return nil
	}
	if s.isOpenAIProviderBlockedBySchedulingThreshold(ctx, fresh) {
		return nil
	}
	if s.isOpenAIProxyStreamQuarantined(ctx, fresh) {
		return nil
	}
	return fresh
}

// parentProviderLookup 返回供 parentHealthyForShadow 使用的母提供商解析闭包:经 providerRepo
// 按 ID 取当前 Provider(repo 为空时 fail-closed 返回 nil)。统一调度/粘连各路径的母提供商解析,
// 取代各调用点重复内联的同一闭包(历史上 recheck 等路径还漏写过 providerRepo==nil 守卫)。
// L2 候选循环改用带 per-pass 缓存的 parentLookupL2,不走此方法。
func (s *Compatible) parentProviderLookup(ctx context.Context) func(int64) *gatewayprovider.ExecutionProvider {
	return func(id int64) *gatewayprovider.ExecutionProvider {
		if s.providerRepo == nil {
			return nil
		}
		a, _ := s.providerRepo.GetByID(ctx, id)
		return a
	}
}

func (s *Compatible) recheckSelectedOpenAIProviderFromDB(ctx context.Context, provider *gatewayprovider.ExecutionProvider, groupID *int64, platform string, requestedModel string, requireCompact bool, requiredCapability providercore.OpenAIEndpointCapability) *gatewayprovider.ExecutionProvider {
	if provider == nil {
		return nil
	}
	platform = strings.TrimSpace(platform)
	if s.schedulerSnapshot == nil || s.providerRepo == nil {
		if !s.openAIProviderMatchesSchedulingGroup(provider, groupID) {
			return nil
		}
		if !s.openAIProviderPassesPrivacyRequirement(ctx, groupID, provider) {
			return nil
		}
		if s.candidateEligibilityReason(ctx, provider, platform, requestedModel, requireCompact, requiredCapability) != "" {
			return nil
		}
		if s.isOpenAIProviderBlockedBySchedulingThreshold(ctx, provider) {
			return nil
		}
		if !s.shadowProtocolsAllowed(ctx, provider) || !providercore.ParentHealthyForShadow(gatewayprovider.ExecutionRecord(provider), func(id int64) *providercore.Record {
			return gatewayprovider.ExecutionRecord(s.parentProviderLookup(ctx)(id))
		}) {
			return nil
		}
		if s.isOpenAIProxyStreamQuarantined(ctx, provider) {
			return nil
		}
		return provider
	}

	latest, err := s.providerRepo.GetByID(ctx, provider.Record.ID)
	if err != nil || latest == nil {
		return nil
	}
	if !s.openAIProviderMatchesSchedulingGroup(latest, groupID) {
		return nil
	}
	if !s.openAIProviderPassesPrivacyRequirement(ctx, groupID, latest) {
		return nil
	}
	if s.candidateEligibilityReason(ctx, latest, platform, requestedModel, requireCompact, requiredCapability) != "" {
		return nil
	}
	if !s.shadowProtocolsAllowed(ctx, latest) || !providercore.ParentHealthyForShadow(gatewayprovider.ExecutionRecord(latest), func(id int64) *providercore.Record {
		return gatewayprovider.ExecutionRecord(s.parentProviderLookup(ctx)(id))
	}) {
		return nil
	}
	if s.isOpenAIProviderRequestRuntimeBlocked(latest, requestedModel) {
		return nil
	}
	if s.isOpenAIProviderBlockedBySchedulingThreshold(ctx, latest) {
		return nil
	}
	if s.isOpenAIProxyStreamQuarantined(ctx, latest) {
		return nil
	}
	return latest
}

func (s *Compatible) openAIProviderMatchesSchedulingGroup(provider *gatewayprovider.ExecutionProvider, groupID *int64) bool {
	return openAIStickyProviderMatchesGroup(provider, groupID)
}

// openAIProviderPassesPrivacyRequirement 判断提供商是否满足当前分组的隐私资格。
func (s *Compatible) openAIProviderPassesPrivacyRequirement(ctx context.Context, groupID *int64, provider *gatewayprovider.ExecutionProvider) bool {
	return provider != nil && (!s.openAIGroupRequiresPrivacySet(ctx, groupID) || provider.View().IsPrivacySet())
}

func (s *Compatible) getSchedulableProvider(ctx context.Context, providerID int64) (*gatewayprovider.ExecutionProvider, error) {
	var (
		provider *gatewayprovider.ExecutionProvider
		err      error
	)
	if s.schedulerSnapshot != nil {
		provider, err = readSnapshotProvider(ctx, s.schedulerSnapshot, providerID)
	} else {
		provider, err = s.providerRepo.GetByID(ctx, providerID)
	}
	if err != nil || provider == nil {
		return provider, err
	}
	if s.isOpenAIProviderBlockedBySchedulingThreshold(ctx, provider) {
		return nil, nil
	}

	if provider.View().IsGrok() {
		if gated := s.filterGrokFreeQuotaProvidersForOpenAI(ctx, []gatewayprovider.ExecutionProvider{*provider}); len(gated) == 0 {
			return nil, nil
		}
	}
	return provider, nil
}

// filterGrokFreeQuotaProvidersForOpenAI 为 OpenAI 兼容基础选择路径应用与
// 本地免费层软性限制与通用选择使用同一规则。
func (s *Compatible) filterGrokFreeQuotaProvidersForOpenAI(ctx context.Context, providers []gatewayprovider.ExecutionProvider) []gatewayprovider.ExecutionProvider {
	if s == nil {
		return providers
	}
	return filterFreeQuotaProjection(s.freeQuotaGate, providers)
}

func (s *Compatible) filterOpenAIProvidersBySchedulingThreshold(ctx context.Context, providers []gatewayprovider.ExecutionProvider) []gatewayprovider.ExecutionProvider {
	if len(providers) == 0 {
		return providers
	}

	filtered := make([]gatewayprovider.ExecutionProvider, 0, len(providers))
	for i := range providers {
		if s.isOpenAIProviderBlockedBySchedulingThreshold(ctx, &providers[i]) {
			continue
		}
		filtered = append(filtered, providers[i])
	}
	return filtered
}

func (s *Compatible) isOpenAIProviderBlockedBySchedulingThreshold(ctx context.Context, provider *gatewayprovider.ExecutionProvider) bool {
	if s == nil || s.healthObserver == nil || provider == nil {
		return false
	}
	return gatewayprovider.ApplyExecutionSchedulingThreshold(ctx, s.healthObserver, provider)
}

func (s *Compatible) hydrateSelectedProvider(ctx context.Context, provider *gatewayprovider.ExecutionProvider) (*gatewayprovider.ExecutionProvider, error) {
	if provider == nil || s.schedulerSnapshot == nil || s.providerRepo != nil {
		return provider, nil
	}
	hydrated, err := readSnapshotProvider(ctx, s.schedulerSnapshot, provider.Record.ID)
	if err != nil {
		return nil, err
	}
	if hydrated == nil {
		return nil, fmt.Errorf("selected openai provider %d not found during hydration", provider.Record.ID)
	}
	return hydrated, nil
}

func (s *Compatible) newSelectionResult(ctx context.Context, provider *gatewayprovider.ExecutionProvider, acquired bool, release func(), waitPlan *schedulercore.ProviderWaitPlan) (*gatewayprovider.SelectionResult, error) {
	hydrated, err := s.hydrateSelectedProvider(ctx, provider)
	if err != nil {
		return nil, err
	}
	return &gatewayprovider.SelectionResult{
		Provider:    hydrated,
		Acquired:    acquired,
		ReleaseFunc: release,
		WaitPlan:    waitPlan,
	}, nil
}

func (s *Compatible) newAcquiredSelectionResult(ctx context.Context, provider *gatewayprovider.ExecutionProvider, release func()) (*gatewayprovider.SelectionResult, error) {
	selection, err := s.newSelectionResult(ctx, provider, true, release, nil)
	if err != nil && release != nil {
		release()
	}
	return selection, err
}

func (s *Compatible) schedulingConfig() schedulercore.FlowOptions {
	return s.options.Scheduling
}

// StickyProviderID 只读取当前分组会话的绑定，用于请求开始时固定缓存计费依据。
func (s *Compatible) StickyProviderID(ctx context.Context, groupID *int64, sessionHash string) int64 {
	if s == nil || s.cache == nil || groupID == nil || *groupID <= 0 || sessionHash == "" {
		return 0
	}
	id, err := s.getStickySessionProviderID(ctx, groupID, sessionHash)
	if err != nil {
		return 0
	}
	return id
}

// readSchedulingGroup 通过配置的读取函数获取兼容请求的调度分组。
func (s *Compatible) readSchedulingGroup(ctx context.Context, id int64) (*routing.Group, error) {
	if s.schedulingGroups == nil {
		return nil, nil
	}
	return s.schedulingGroups(ctx, id)
}
