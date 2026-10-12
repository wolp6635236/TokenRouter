package selection

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	schedulercore "github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/scheduler/policy"
)

type selectionFailureStats struct {
	Total              int
	Eligible           int
	Excluded           int
	Unschedulable      int
	PlatformFiltered   int
	ModelUnsupported   int
	ModelRateLimited   int
	SamplePlatformIDs  []int64
	SampleMappingIDs   []int64
	SampleRateLimitIDs []string
}

type selectionFailureDiagnosis struct {
	Category string
	Detail   string
}

// SelectProviderForModel 选择支持指定模型的提供商（粘性会话+优先级+模型映射）。
func (s *Generic) SelectProviderForModel(ctx context.Context, groupID *int64, sessionHash string, requestedModel string) (*gatewayprovider.ExecutionProvider, error) {
	return s.SelectProviderForModelWithExclusions(ctx, groupID, sessionHash, requestedModel, nil)
}

// SelectProviderForModelWithExclusions 选择支持请求模型且不在排除集合中的提供商。
func (s *Generic) SelectProviderForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*gatewayprovider.ExecutionProvider, error) {
	ctx = withSelectionRequest(ctx, groupID, requestedModel)
	core, scope := s.genericSelector()
	selected, err := core.SelectOnly(ctx, schedulercore.SelectionInput{GroupID: groupID, SessionHash: sessionHash, RequestedModel: requestedModel, ExcludedIDs: excludedIDs})
	return scope.oldProvider(selected), err
}

// SelectProviderWithLoadAwareness selects provider with load-awareness and wait plan.
// metadataUserID: 用于客户端亲和调度，从中提取客户端 ID
// tokenRouterUserID: 系统用户 ID，用于二维亲和调度
// @project-doc docs/architecture/gateway_request_lifecycle.md#account_selection_and_failover
func (s *Generic) SelectProviderWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, tokenRouterUserID int64) (*gatewayprovider.SelectionResult, error) {
	ctx = withSelectionRequest(ctx, groupID, requestedModel)
	core, scope := s.genericSelector()
	plan, _ := requeststate.RoutePlanFromContext(ctx)
	result, err := core.Select(ctx, schedulercore.SelectionInput{RoutePlan: plan, GroupID: groupID, SessionHash: sessionHash, RequestedModel: requestedModel, ExcludedIDs: excludedIDs})
	return scope.restore(result), err
}

// ReportAdvancedProviderScheduleResult 将通用网关转发结果写入高级调度运行时反馈。
// 高级调度器选出的请求更新统计。
func (s *Generic) ReportAdvancedProviderScheduleResult(selection *gatewayprovider.SelectionResult, providerID int64, success bool, result *forwardcore.MessagesResult) {
	if s == nil || selection == nil || !selection.AdvancedScheduler || providerID <= 0 {
		return
	}
	var firstTokenMs *int
	if result != nil {
		firstTokenMs = result.FirstTokenMs
	}
	if selection.AdvancedSchedulerFeedback != nil {
		s.advancedSchedulerStats().Report(providerID, success, firstTokenMs, *selection.AdvancedSchedulerFeedback)
		return
	}
	s.advancedSchedulerStats().Report(providerID, success, firstTokenMs)
}

// RecordAdvancedProviderSwitch 记录高级调度请求的一次提供商切换事件。
func (s *Generic) RecordAdvancedProviderSwitch(selection *gatewayprovider.SelectionResult) {
	if s == nil || selection == nil || !selection.AdvancedScheduler {
		return
	}
	s.advancedSchedulerStats().ReportSwitch()
}

// advancedSchedulerStats 返回网关级运行时反馈；测试或旧构造路径未初始化时惰性补齐。
func (s *Generic) advancedSchedulerStats() *schedulercore.RuntimeStats {
	if s == nil {
		return nil
	}
	if s.advancedProviderStats == nil {
		s.advancedProviderStats = schedulercore.NewRuntimeStats(time.Now)
	}
	return s.advancedProviderStats
}

// advancedSchedulerEffectiveSettingsForRequest 将最终分组覆盖应用到网关通用设置之上。
func (s *Generic) advancedSchedulerEffectiveSettingsForRequest(ctx context.Context, id *int64) policy.EffectiveSettings {
	if ctx == nil {
		ctx = context.Background()
	}
	group := schedulerRequestGroup(ctx, id, s.schedulerSnapshot != nil, s.readSchedulingGroup)
	return s.schedulerParameters.Effective(ctx, schedulerGroupOverrides(group))
}

func (s *Generic) schedulingConfig() schedulercore.FlowOptions {
	return s.options.Scheduling
}

func (s *Generic) withGroupContext(ctx context.Context, group *routing.Group) context.Context {
	if !routing.IsGroupContextValid(group) {
		return ctx
	}
	if existing, ok := requeststate.GroupFromContext(ctx); ok && existing != nil && existing.ID == group.ID && routing.IsGroupContextValid(existing) {
		return ctx
	}
	return requeststate.WithGroup(ctx, group)
}

func (s *Generic) groupFromContext(ctx context.Context, groupID int64) *routing.Group {
	if group, ok := requeststate.GroupFromContext(ctx); ok && routing.IsGroupContextValid(group) && group.ID == groupID {
		return group
	}
	return nil
}

func (s *Generic) resolveGroupByID(ctx context.Context, groupID int64) (*routing.Group, error) {
	if group := s.groupFromContext(ctx, groupID); group != nil {
		return group, nil
	}
	group, err := s.groupRepo.GetByIDLite(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("get group failed: %w", err)
	}
	return group, nil
}

func (s *Generic) routingProviderIDsForRequest(ctx context.Context, groupID *int64, requestedModel string, platform string) []int64 {
	if groupID == nil || requestedModel == "" {
		return nil
	}
	group, err := s.resolveGroupByID(ctx, *groupID)
	if err != nil || group == nil {
		if s.debugModelRoutingEnabled() {
			logging.LegacyPrintf("service.gateway", "[ModelRoutingDebug] resolve group failed: group_id=%v model=%s platform=%s err=%v", derefGroupID(groupID), requestedModel, platform, err)
		}
		return nil
	}

	routingModel := s.groupMappedModelForGroup(ctx, groupID, requestedModel)
	ids := group.GetRoutingProviderIDs(routingModel)
	if s.debugModelRoutingEnabled() {
		logging.LegacyPrintf("service.gateway", "[ModelRoutingDebug] routing lookup: group_id=%d model=%s enabled=%v rules=%d matched_ids=%v",
			group.ID, requestedModel, group.ModelRoutingEnabled, len(group.ModelRouting), ids)
	}
	return ids
}

func (s *Generic) resolveGatewayGroup(ctx context.Context, groupID *int64) (*routing.Group, *int64, error) {
	group, err := currentSelectionGroup(ctx, groupID, s.resolveGroupByID)
	if err != nil {
		return nil, nil, err
	}
	return group, groupID, nil
}

// checkClaudeCodeRestriction 检查当前组的客户端限制，选择器不自行切换分组。
func (s *Generic) checkClaudeCodeRestriction(ctx context.Context, groupID *int64) (*routing.Group, *int64, error) {
	if groupID == nil {
		return nil, groupID, nil
	}

	group, resolvedID, err := s.resolveGatewayGroup(ctx, groupID)
	if err != nil {
		return nil, nil, err
	}

	return group, resolvedID, nil
}

func (s *Generic) resolvePlatform(ctx context.Context, groupID *int64, group *routing.Group) (string, bool, error) {
	if platform, forced := apikey.ForcePlatformFromContext(ctx); forced && platform != "" {
		return platform, true, nil
	}
	return "", false, nil
}

func (s *Generic) listSchedulableProviders(ctx context.Context, groupID *int64, platform string, hasForcePlatform bool) ([]gatewayprovider.ExecutionProvider, bool, error) {
	if groupID == nil || *groupID <= 0 {
		return nil, false, nil
	}
	var providers []gatewayprovider.ExecutionProvider
	var err error
	if s.schedulerSnapshot != nil {
		providers, _, err = readSnapshotProviders(ctx, s.schedulerSnapshot, groupID, platform, hasForcePlatform)
	} else if platform == "" {
		providers, err = s.providerRepo.ListSchedulableByGroupIDAndPlatforms(ctx, *groupID, capability.ProviderPlatforms())
	} else {
		providers, err = s.providerRepo.ListSchedulableByGroupIDAndPlatform(ctx, *groupID, platform)
	}
	if err != nil {
		return nil, false, err
	}
	providers = s.filterProvidersBySchedulingThreshold(ctx, providers)
	providers = s.filterGrokFreeQuotaProvidersForGateway(ctx, providers)
	return providers, false, nil
}

// IsSingleAntigravityProviderGroup 检查指定分组是否只有一个 antigravity 平台的可调度提供商。
// 用于 Handler 层在首次请求时提前设置 SingleProviderRetry context，
// 避免单提供商分组收到 503 时错误地设置模型限流标记导致后续请求连续快速失败。
func (s *Generic) IsSingleAntigravityProviderGroup(ctx context.Context, groupID *int64) bool {
	providers, _, err := s.listSchedulableProviders(ctx, groupID, capability.PlatformAntigravity, true)
	if err != nil {
		return false
	}
	return len(providers) == 1
}

func (s *Generic) isProviderAllowedForPlatform(provider *gatewayprovider.ExecutionProvider, platform string, useMixed bool) bool {
	return provider != nil && (platform == "" || provider.Record.Platform == platform)
}

func (s *Generic) isProviderSchedulableForSelection(provider *gatewayprovider.ExecutionProvider) bool {
	if provider == nil {
		return false
	}
	return provider.View().IsSchedulable()
}

func (s *Generic) isProviderSchedulableForModelSelection(ctx context.Context, provider *gatewayprovider.ExecutionProvider, requestedModel string) bool {
	if provider == nil {
		return false
	}
	routingModel := s.groupMappedModelForProviderLayer(ctx, requestedModel)
	return gatewayprovider.ExecutionModelPolicy(provider).Schedulable(ctx, routingModel)
}

// shouldClearStickySessionForProviderLayer 使用分组映射后的模型检查粘性提供商模型限流。
func (s *Generic) shouldClearStickySessionForProviderLayer(ctx context.Context, provider *gatewayprovider.ExecutionProvider, requestedModel string) bool {
	routingModel := s.groupMappedModelForProviderLayer(ctx, requestedModel)
	return shouldClearStickySession(provider, routingModel)
}

// isProviderEligibleExceptModelSupport 判断提供商除模型白名单/映射外是否具备本次请求资格。
func (s *Generic) isProviderEligibleExceptModelSupport(ctx context.Context, provider *gatewayprovider.ExecutionProvider, requestedModel string, platform string, excludedIDs map[int64]struct{}, useMixed bool, groupID *int64, schedGroup *routing.Group) bool {
	if !gatewayprovider.ExecutionModelPolicy(provider).AllowsProtocol(ctx) {
		return false
	}
	if provider == nil {
		return false
	}
	if excludedIDs != nil {
		if _, excluded := excludedIDs[provider.Record.ID]; excluded {
			return false
		}
	}
	if !s.isProviderSchedulableForSelection(provider) || !s.isProviderAllowedForPlatform(provider, platform, useMixed) {
		return false
	}
	if schedGroup != nil && schedGroup.RequirePrivacySet && !provider.View().IsPrivacySet() {
		return false
	}
	if !s.isProviderSchedulableForQuota(provider) || !s.isProviderSchedulableForWindowCost(ctx, provider, false) || !s.isProviderSchedulableForRPM(ctx, provider, false) {
		return false
	}
	if groupID != nil && s.needsUpstreamGroupRestrictionCheck(ctx, groupID) &&
		s.isUpstreamModelRestrictedByGroup(ctx, *groupID, provider, requestedModel) {
		return false
	}
	return true
}

// shouldUseGroupModelUnsupportedError 判断提供商选择失败是否明确由分组模型限制导致。
func (s *Generic) shouldUseGroupModelUnsupportedError(ctx context.Context, providers []gatewayprovider.ExecutionProvider, requestedModel string, platform string, excludedIDs map[int64]struct{}, useMixed bool, groupID *int64, schedGroup *routing.Group) bool {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" || len(providers) == 0 {
		return false
	}
	hasRelevantProvider := false
	for i := range providers {
		acc := &providers[i]
		if !s.isProviderEligibleExceptModelSupport(ctx, acc, requestedModel, platform, excludedIDs, useMixed, groupID, schedGroup) {
			continue
		}
		hasRelevantProvider = true
		if s.isModelSupportedByProviderWithContext(ctx, acc, requestedModel) {
			return false
		}
	}
	return hasRelevantProvider
}

// groupModelUnsupportedErrorIfApplicable 在确认是分组模型限制时返回 typed error。
func (s *Generic) groupModelUnsupportedErrorIfApplicable(ctx context.Context, providers []gatewayprovider.ExecutionProvider, requestedModel string, platform string, excludedIDs map[int64]struct{}, useMixed bool, groupID *int64, schedGroup *routing.Group) error {
	if s.shouldUseGroupModelUnsupportedError(ctx, providers, requestedModel, platform, excludedIDs, useMixed, groupID, schedGroup) {
		if err := routing.NewGroupModelRejection(platform, requestedModel, modelRejectionSources(providers)); err != nil {
			return err
		}
	}
	return nil
}

// isProviderInGroup checks if the provider belongs to the specified group.
// When groupID is nil, returns true only for ungrouped providers (no group assignments).
func (s *Generic) isProviderInGroup(provider *gatewayprovider.ExecutionProvider, groupID *int64) bool {
	return openAIStickyProviderMatchesGroup(provider, groupID)
}

func (s *Generic) tryAcquireProviderSlot(ctx context.Context, providerID int64, maxConcurrency int) (*schedulercore.AcquireResult, error) {
	if schedulercore.IsSelectOnly(ctx) {
		return &schedulercore.AcquireResult{Acquired: true, ReleaseFunc: func() {}}, nil
	}
	if s.concurrencyService == nil {
		return &schedulercore.AcquireResult{Acquired: true, ReleaseFunc: func() {}}, nil
	}
	return s.concurrencyService.AcquireProviderSlot(ctx, providerID, maxConcurrency)
}

func (s *Generic) withWindowCostPrefetch(ctx context.Context, providers []gatewayprovider.ExecutionProvider) context.Context {
	if ctx == nil || len(providers) == 0 || !s.windowPrefetchAvailable {
		return ctx
	}
	values := make([]billing.CostWindowInput, len(providers))
	for i := range providers {
		values[i] = costWindowInput(&providers[i])
	}
	return s.windowCostGuard().Prefetch(ctx, values)
}

// isProviderSchedulableForQuota 检查提供商是否在配额限制内
// 适用于配置了 quota_limit 的 apikey 和 bedrock 类型提供商。
func (s *Generic) isProviderSchedulableForQuota(provider *gatewayprovider.ExecutionProvider) bool {
	if !provider.View().IsAPIKeyOrBedrock() {
		return true
	}
	return !provider.View().IsQuotaExceeded()
}

// isProviderSchedulableForWindowCost 检查提供商是否可根据窗口费用进行调度
// 仅适用于 Anthropic OAuth/SetupToken 提供商
// 返回 true 表示可调度，false 表示不可调度。
func (s *Generic) isProviderSchedulableForWindowCost(ctx context.Context, provider *gatewayprovider.ExecutionProvider, sticky bool) bool {
	return s.windowCostGuard().Allow(ctx, costWindowInput(provider), sticky)
}

// withRPMPrefetch 批量预取所有候选提供商的 RPM 计数。
func (s *Generic) withRPMPrefetch(ctx context.Context, providers []gatewayprovider.ExecutionProvider) context.Context {
	if s.rpmCache == nil {
		return ctx
	}

	var ids []int64
	for i := range providers {
		if providers[i].View().IsAnthropicOAuthOrSetupToken() && gatewayprovider.ExecutionRuntimeConfig(&providers[i]).GetBaseRPM() > 0 {
			ids = append(ids, providers[i].Record.ID)
		}
	}
	return schedulercore.PrefetchRPM(ctx, s.rpmCache, ids)
}

// isProviderSchedulableForRPM 检查提供商是否可根据 RPM 进行调度
// 仅适用于 Anthropic OAuth/SetupToken 提供商。
func (s *Generic) isProviderSchedulableForRPM(ctx context.Context, provider *gatewayprovider.ExecutionProvider, sticky bool) bool {
	if !provider.View().IsAnthropicOAuthOrSetupToken() {
		return true
	}
	return schedulercore.AllowProviderRPM(ctx, s.rpmCache, schedulercore.ProviderRPMInput{ID: provider.Record.ID, Enabled: true, Base: gatewayprovider.ExecutionRuntimeConfig(provider).GetBaseRPM(), Buffer: gatewayprovider.ExecutionRuntimeConfig(provider).GetRPMStickyBuffer(), Strategy: gatewayprovider.ExecutionRuntimeConfig(provider).GetRPMStrategy()}, sticky)
}

// IncrementProviderRPM increments the RPM counter for the given provider.
// 已知 TOCTOU 竞态：调度时读取 RPM 计数与此处递增之间存在时间窗口，
// 高并发下可能短暂超出 RPM 限制。这是与 WindowCost 一致的 soft-limit
// 并发检查允许少量超额，以减少加锁延迟。
func (s *Generic) IncrementProviderRPM(ctx context.Context, id int64) error {
	return schedulercore.IncrementProviderRPM(ctx, s.rpmCache, id)
}

// checkAndRegisterSession 检查并注册会话，用于会话数量限制
// 仅适用于 Anthropic OAuth/SetupToken 提供商
// sessionID: 会话标识符（使用粘性会话的 hash）
// 返回 true 表示允许（在限制内或会话已存在），false 表示拒绝（超出限制且是新会话）。
func (s *Generic) checkAndRegisterSession(ctx context.Context, provider *gatewayprovider.ExecutionProvider, session string) bool {
	if schedulercore.IsSelectOnly(ctx) {
		return true
	}
	return schedulercore.RegisterSession(ctx, s.sessionLimitCache, schedulerSessionBinding(provider, session))
}

// ReleaseProviderSession 立即释放会话槽（不等待空闲超时）
// 供 handler 在请求最终失败（选号成功但转发失败/客户端中断）时调用：
// 上游从未真正服务该会话，若继续占槽，max_sessions 受限的提供商会被失败请求的
// session hash 卡满整个空闲窗口，后续新会话全部被拒。
// 适用条件与 checkAndRegisterSession 对齐；不适用提供商为 no-op，幂等可安全重复调用。
func (s *Generic) ReleaseProviderSession(ctx context.Context, provider *gatewayprovider.ExecutionProvider, session string) {
	if s == nil {
		return
	}
	schedulercore.FinishSession(ctx, s.sessionLimitCache, schedulerSessionBinding(provider, session), schedulercore.AttemptOutcome{}, schedulercore.Diagnostics{
		Logf: logging.LegacyPrintf,

		Event: logging.Event,
	},
	)
}

func schedulerSessionBinding(provider *gatewayprovider.ExecutionProvider, session string) schedulercore.SessionBinding {
	if provider == nil {
		return schedulercore.SessionBinding{}
	}
	return schedulercore.SessionBinding{ProviderID: provider.Record.ID, SessionID: session, Enabled: provider.View().IsAnthropicOAuthOrSetupToken(), Limit: gatewayprovider.ExecutionRuntimeConfig(provider).GetMaxSessions(), IdleTimeout: time.Duration(gatewayprovider.ExecutionRuntimeConfig(provider).GetSessionIdleTimeoutMinutes()) * time.Minute}
}

func (s *Generic) getSchedulableProvider(ctx context.Context, providerID int64) (*gatewayprovider.ExecutionProvider, error) {
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
	if s.isProviderBlockedBySchedulingThreshold(ctx, provider) {
		return nil, nil
	}

	if provider.View().IsGrok() {
		if gated := s.filterGrokFreeQuotaProvidersForGateway(ctx, []gatewayprovider.ExecutionProvider{*provider}); len(gated) == 0 {
			return nil, nil
		}
	}
	return provider, nil
}

func (s *Generic) filterProvidersBySchedulingThreshold(ctx context.Context, providers []gatewayprovider.ExecutionProvider) []gatewayprovider.ExecutionProvider {
	if len(providers) == 0 {
		return providers
	}

	filtered := make([]gatewayprovider.ExecutionProvider, 0, len(providers))
	for i := range providers {
		if s.isProviderBlockedBySchedulingThreshold(ctx, &providers[i]) {
			continue
		}
		filtered = append(filtered, providers[i])
	}
	return filtered
}

func (s *Generic) isProviderBlockedBySchedulingThreshold(ctx context.Context, provider *gatewayprovider.ExecutionProvider) bool {
	if s == nil || s.healthObserver == nil || provider == nil {
		return false
	}
	return gatewayprovider.ApplyExecutionSchedulingThreshold(ctx, s.healthObserver, provider)
}

func (s *Generic) hydrateSelectedProvider(ctx context.Context, provider *gatewayprovider.ExecutionProvider) (*gatewayprovider.ExecutionProvider, error) {
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
		model := s.groupMappedModelForGroup(ctx, groupID, input.model)
		policy := gatewayprovider.ExecutionModelPolicy(hydrated)
		if !openAIStickyProviderMatchesGroup(hydrated, groupID) || !policy.Schedulable(ctx, model) || !policy.Supports(ctx, model) {
			return nil, schedulercore.ErrNoAvailableProviders
		}
	}
	return hydrated, nil
}

func (s *Generic) newSelectionResult(ctx context.Context, provider *gatewayprovider.ExecutionProvider, acquired bool, release func(), waitPlan *schedulercore.ProviderWaitPlan) (*gatewayprovider.SelectionResult, error) {
	attempt := schedulercore.NewAttemptLease(schedulercore.RequestLease(ctx), nil, release)
	if release != nil {
		release = attempt.Release
	}

	hydrated, err := s.hydrateSelectedProvider(ctx, provider)
	if err != nil {
		if release != nil {
			release()
		}
		return nil, err
	}
	selection := &gatewayprovider.SelectionResult{
		Provider:    hydrated,
		Acquired:    acquired,
		ReleaseFunc: release,
		WaitPlan:    waitPlan,
	}
	if group, ok := requeststate.GroupFromContext(ctx); ok && routing.IsGroupContextValid(group) && group.UsesAdvancedScheduler() {
		selection.AdvancedScheduler = true
		feedback := s.advancedSchedulerEffectiveSettingsForRequest(ctx, &group.ID).Feedback
		selection.AdvancedSchedulerFeedback = &feedback
	}
	return selection, nil
}

func (s *Generic) logDetailedSelectionFailure(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	requestedModel string,
	platform string,
	providers []gatewayprovider.ExecutionProvider,
	excludedIDs map[int64]struct{},
	allowMixedScheduling bool,
) selectionFailureStats {
	stats := s.collectSelectionFailureStats(ctx, providers, requestedModel, platform, excludedIDs, allowMixedScheduling)
	logging.LegacyPrintf(
		"service.gateway",
		"[SelectProviderDetailed] group_id=%v model=%s platform=%s session=%s total=%d eligible=%d excluded=%d unschedulable=%d platform_filtered=%d model_unsupported=%d model_rate_limited=%d sample_platform_filtered=%v sample_model_unsupported=%v sample_model_rate_limited=%v",
		derefGroupID(groupID),
		requestedModel,
		platform,
		shortSessionHash(sessionHash),
		stats.Total,
		stats.Eligible,
		stats.Excluded,
		stats.Unschedulable,
		stats.PlatformFiltered,
		stats.ModelUnsupported,
		stats.ModelRateLimited,
		stats.SamplePlatformIDs,
		stats.SampleMappingIDs,
		stats.SampleRateLimitIDs,
	)
	return stats
}

func (s *Generic) collectSelectionFailureStats(
	ctx context.Context,
	providers []gatewayprovider.ExecutionProvider,
	requestedModel string,
	platform string,
	excludedIDs map[int64]struct{},
	allowMixedScheduling bool,
) selectionFailureStats {
	stats := selectionFailureStats{
		Total: len(providers),
	}

	for i := range providers {
		acc := &providers[i]
		diagnosis := s.diagnoseSelectionFailure(ctx, acc, requestedModel, platform, excludedIDs, allowMixedScheduling)
		switch diagnosis.Category {
		case "excluded":
			stats.Excluded++
		case "unschedulable":
			stats.Unschedulable++
		case "platform_filtered":
			stats.PlatformFiltered++
			stats.SamplePlatformIDs = appendSelectionFailureSampleID(stats.SamplePlatformIDs, acc.Record.ID)
		case "model_unsupported":
			stats.ModelUnsupported++
			stats.SampleMappingIDs = appendSelectionFailureSampleID(stats.SampleMappingIDs, acc.Record.ID)
		case "model_rate_limited":
			stats.ModelRateLimited++
			routingModel := s.groupMappedModelForProviderLayer(ctx, requestedModel)
			remaining := gatewayprovider.ExecutionModelPolicy(acc).LimitRemaining(ctx, routingModel).Truncate(time.Second)
			stats.SampleRateLimitIDs = appendSelectionFailureRateSample(stats.SampleRateLimitIDs, acc.Record.ID, remaining)
		default:
			stats.Eligible++
		}
	}

	return stats
}

func (s *Generic) diagnoseSelectionFailure(
	ctx context.Context,
	acc *gatewayprovider.ExecutionProvider,
	requestedModel string,
	platform string,
	excludedIDs map[int64]struct{},
	allowMixedScheduling bool,
) selectionFailureDiagnosis {
	if acc == nil {
		return selectionFailureDiagnosis{Category: "unschedulable", Detail: "provider_nil"}
	}
	if _, excluded := excludedIDs[acc.Record.ID]; excluded {
		return selectionFailureDiagnosis{Category: "excluded"}
	}
	if !s.isProviderSchedulableForSelection(acc) {
		return selectionFailureDiagnosis{Category: "unschedulable", Detail: "generic_unschedulable"}
	}
	if isPlatformFilteredForSelection(acc, platform, allowMixedScheduling) {
		return selectionFailureDiagnosis{
			Category: "platform_filtered",
			Detail:   fmt.Sprintf("provider_platform=%s requested_platform=%s", acc.Record.Platform, strings.TrimSpace(platform)),
		}
	}
	if requestedModel != "" && !s.isModelSupportedByProviderWithContext(ctx, acc, requestedModel) {
		return selectionFailureDiagnosis{
			Category: "model_unsupported",
			Detail:   fmt.Sprintf("model=%s", requestedModel),
		}
	}
	if !s.isProviderSchedulableForModelSelection(ctx, acc, requestedModel) {
		routingModel := s.groupMappedModelForProviderLayer(ctx, requestedModel)
		remaining := gatewayprovider.ExecutionModelPolicy(acc).LimitRemaining(ctx, routingModel).Truncate(time.Second)
		return selectionFailureDiagnosis{
			Category: "model_rate_limited",
			Detail:   fmt.Sprintf("remaining=%s", remaining),
		}
	}
	return selectionFailureDiagnosis{Category: "eligible"}
}

func isPlatformFilteredForSelection(acc *gatewayprovider.ExecutionProvider, platform string, allowMixedScheduling bool) bool {
	return acc == nil || platform != "" && acc.Record.Platform != platform
}

func appendSelectionFailureSampleID(samples []int64, id int64) []int64 {
	const limit = 5
	if len(samples) >= limit {
		return samples
	}
	return append(samples, id)
}

func appendSelectionFailureRateSample(samples []string, providerID int64, remaining time.Duration) []string {
	const limit = 5
	if len(samples) >= limit {
		return samples
	}
	return append(samples, fmt.Sprintf("%d(%s)", providerID, remaining))
}

func summarizeSelectionFailureStats(stats selectionFailureStats) string {
	return fmt.Sprintf(
		"total=%d eligible=%d excluded=%d unschedulable=%d platform_filtered=%d model_unsupported=%d model_rate_limited=%d",
		stats.Total,
		stats.Eligible,
		stats.Excluded,
		stats.Unschedulable,
		stats.PlatformFiltered,
		stats.ModelUnsupported,
		stats.ModelRateLimited,
	)
}

// isModelSupportedByProviderWithContext 根据分组映射后的模型检查提供商支持能力。
func (s *Generic) isModelSupportedByProviderWithContext(ctx context.Context, provider *gatewayprovider.ExecutionProvider, requestedModel string) bool {
	routingModel := s.groupMappedModelForProviderLayer(ctx, requestedModel)
	return s.isRoutingModelSupportedByProviderWithContext(ctx, provider, routingModel)
}

// isRoutingModelSupportedByProviderWithContext 检查已经过分组映射的模型，避免重复执行分组映射。
func (s *Generic) isRoutingModelSupportedByProviderWithContext(ctx context.Context, provider *gatewayprovider.ExecutionProvider, routingModel string) bool {
	return gatewayprovider.ExecutionModelPolicy(provider).Supports(ctx, routingModel)
}

func (s *Generic) groupMappedModelForProviderLayer(ctx context.Context, requestedModel string) string {
	if s == nil || s.groupPolicies == nil || strings.TrimSpace(requestedModel) == "" {
		return requestedModel
	}
	group, ok := requeststate.GroupFromContext(ctx)
	if !ok || !routing.IsGroupContextValid(group) {
		return requestedModel
	}
	groupID := group.ID
	return s.groupMappedModelForGroup(ctx, &groupID, requestedModel)
}

// NewSessionAttempts 为一次请求提供唯一会话完成集合，不保存全局副本。
func (s *Generic) NewSessionAttempts() *schedulercore.SessionAttempts {
	return schedulercore.NewSessionAttempts(s.sessionLimitCache, schedulercore.Diagnostics{
		Logf: logging.LegacyPrintf,

		Event: logging.Event,
	},
	)
}

// TrackSessionAttempt 根据提供商和会话参数登记本次尝试。
func (s *Generic) TrackSessionAttempt(attempts *schedulercore.SessionAttempts, provider *gatewayprovider.ExecutionProvider, session string) {
	attempts.Track(schedulerSessionBinding(provider, session))
}

// costWindowInput 将提供商的会话窗口和额度上限转换为计费检查输入。
func costWindowInput(a *gatewayprovider.ExecutionProvider) billing.CostWindowInput {
	if a == nil {
		return billing.CostWindowInput{}
	}
	return billing.CostWindowInput{ID: a.Record.ID, Enabled: a.View().IsAnthropicOAuthOrSetupToken(), Limit: gatewayprovider.ExecutionRuntimeConfig(a).GetWindowCostLimit(), Reserve: gatewayprovider.ExecutionRuntimeConfig(a).GetWindowCostStickyReserve(), Start: a.Record.SessionWindowStart, End: a.Record.SessionWindowEnd}
}

// readSchedulingGroup 从通用选择器的分组仓库读取调度配置。
func (s *Generic) readSchedulingGroup(ctx context.Context, id int64) (*routing.Group, error) {
	if s.groupRepo == nil {
		return nil, nil
	}
	return s.groupRepo.GetByID(ctx, id)
}
