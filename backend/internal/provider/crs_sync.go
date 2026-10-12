package provider

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/provider/transfer"
)

// CRSProviderStore 提供同步配置、影子关系和凭据写入操作。
type CRSProviderStore interface {
	ShadowProxyStore
	Create(context.Context, *Record) error
	GetByCRSAccountID(context.Context, string) (*Record, error)
	ListCRSAccountIDs(context.Context) (map[string]int64, error)
}
type CRSProxyStore interface {
	ListActive(context.Context) ([]egress.Proxy, error)
	Create(context.Context, *egress.Proxy) error
}
type CRSExporter interface {
	Fetch(context.Context, string, string, string) (*transfer.CRSExportResponse, error)
}
type CRSOptions struct {
	Now     func() time.Time
	Warn    func(string, ...any)
	Refresh func(context.Context, *Record) error
}

// CRSSync 按六类来源逐项同步和预览，通过接口执行网络登录与凭据交换。
type CRSSync struct {
	providerRepo CRSProviderStore
	proxyRepo    CRSProxyStore
	exporter     CRSExporter
	options      CRSOptions
}

type SyncFromCRSInput struct {
	BaseURL             string
	Username            string
	Password            string
	SyncProxies         bool
	SelectedProviderIDs []string // nil 创建全部；显式空只更新已存在提供商
}

type SyncFromCRSItemResult struct {
	CRSAccountID string `json:"crs_account_id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Action       string `json:"action"` // 创建、更新、失败或跳过
	Error        string `json:"error,omitempty"`
}

type SyncFromCRSResult struct {
	Created int                     `json:"created"`
	Updated int                     `json:"updated"`
	Skipped int                     `json:"skipped"`
	Failed  int                     `json:"failed"`
	Items   []SyncFromCRSItemResult `json:"items"`
}

// PreviewFromCRSResult 保持原同步预览计数与逐项列表。
type PreviewFromCRSResult struct {
	NewProviders      []CRSPreviewProvider `json:"new_providers"`
	ExistingProviders []CRSPreviewProvider `json:"existing_providers"`
}

// CRSPreviewProvider 只包含预览展示字段。
type CRSPreviewProvider struct {
	CRSAccountID string `json:"crs_account_id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Platform     string `json:"platform"`
	Type         string `json:"type"`
}

func NewCRSSync(providers CRSProviderStore, proxies CRSProxyStore, exporter CRSExporter, options CRSOptions) *CRSSync {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Warn == nil {
		options.Warn = func(string, ...any) {}
	}
	return &CRSSync{providers, proxies, exporter, options}
}

// refreshOAuthToken 在导入提交后尝试刷新，失败时该项仍计为导入成功。
func (s *CRSSync) refreshOAuthToken(ctx context.Context, value *Record) {
	if s.options.Refresh != nil {
		_ = s.options.Refresh(ctx, value)
	}
}

// GuardCRSShadowParentInvariant 要求有 Spark 影子的母提供商保持 OpenAI OAuth，与管理更新使用相同约束。
// CRS 按全局 crs_account_id 查找记录；跨平台或类型的 ID 碰撞可能使同步分支改写母提供商的 Platform/Type，
// 导致影子无法解析凭据。因此必须同时检查目标平台和类型；修改为其他组合前须先删除影子。
// 返回错误时，调用方跳过该提供商更新并将条目标记为 failed。
func GuardCRSShadowParentInvariant(ctx context.Context, repo ShadowProxyStore, existing *Record, newPlatform, newType string) error {
	if existing == nil {
		return nil
	}
	// 目标仍是合法影子父(OpenAI OAuth)→ 放行(常见:OpenAI OAuth 分支重新同步母提供商),免去一次查询。
	if newPlatform == PlatformOpenAI && newType == ProviderTypeOAuth {
		return nil
	}
	shadows, err := repo.ListShadowsByParent(ctx, existing.ID)
	if err != nil {
		return fmt.Errorf("check spark shadows for crs update: %w", err)
	}
	if len(shadows) > 0 {
		return fmt.Errorf("cannot change a spark-shadow parent provider to %s/%s; it must stay OpenAI OAuth (delete the shadow first)", newPlatform, newType)
	}
	return nil
}

func (s *CRSSync) SyncFromCRS(ctx context.Context, input SyncFromCRSInput) (*SyncFromCRSResult, error) {
	exported, err := s.exporter.Fetch(ctx, input.BaseURL, input.Username, input.Password)
	if err != nil {
		return nil, err
	}

	now := s.options.Now().UTC().Format(time.RFC3339)

	result := &SyncFromCRSResult{
		Items: make(
			[]SyncFromCRSItemResult,
			0,
			len(exported.Data.ClaudeProviders)+len(exported.Data.ClaudeConsoleProviders)+len(exported.Data.OpenAIOAuthProviders)+len(exported.Data.OpenAIResponsesProviders)+len(exported.Data.GeminiOAuthProviders)+len(exported.Data.GeminiAPIKeyProviders),
		),
	}

	selectedSet := CRSBuildSelectedSet(input.SelectedProviderIDs)

	var proxies []egress.Proxy
	if input.SyncProxies {
		proxies, _ = s.proxyRepo.ListActive(ctx)
	}

	// Claude 来源转换为 Anthropic OAuth 或 Setup Token。
	for _, src := range exported.Data.ClaudeProviders {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		targetType := strings.TrimSpace(src.AuthType)
		if targetType == "" {
			targetType = "oauth"
		}
		if targetType != ProviderTypeOAuth && targetType != ProviderTypeSetupToken {
			item.Action = "skipped"
			item.Error = "unsupported authType: " + targetType
			result.Skipped++
			result.Items = append(result.Items, item)
			continue
		}

		accessToken, _ := src.Credentials["access_token"].(string)
		if strings.TrimSpace(accessToken) == "" {
			item.Action = "failed"
			item.Error = "missing access_token"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		proxyID, err := egress.MatchOrCreateCRSProxy(ctx, s.proxyRepo, input.SyncProxies, &proxies, src.Proxy, fmt.Sprintf("crs-%s", src.Name))
		if err != nil {
			item.Action = "failed"
			item.Error = "proxy sync failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := CRSSanitizeCredentialsMap(src.Credentials)
		// Claude 基地址去除末尾 /v1。
		CRSCleanBaseURL(credentials, "/v1")
		// 将 ISO 到期值转换为 Unix 时间戳。
		if expiresAtStr, ok := credentials["expires_at"].(string); ok && expiresAtStr != "" {
			if t, err := time.Parse(time.RFC3339, expiresAtStr); err == nil {
				credentials["expires_at"] = t.Unix()
			}
		}
		// 未提供预热拦截开关时保持默认 false。
		if _, exists := credentials["intercept_warmup_requests"]; !exists {
			credentials["intercept_warmup_requests"] = false
		}
		priority := CRSClampPriority(src.Priority)
		concurrency := 3
		status := MapCRSStatus(src.IsActive, src.Status)

		// 保留来源扩展字段并补入同步元数据。
		extra := make(map[string]any)
		if src.Extra != nil {
			maps.Copy(extra, src.Extra)
		}
		extra["crs_account_id"] = src.ID
		extra["crs_kind"] = src.Kind
		extra["crs_synced_at"] = now
		// 从凭据读取组织和提供商 ID，写入扩展信息。
		if orgUUID, ok := src.Credentials["org_uuid"]; ok {
			extra["org_uuid"] = orgUUID
		}
		if providerUUID, ok := src.Credentials["account_uuid"]; ok {
			extra["account_uuid"] = providerUUID
		}

		existing, err := s.providerRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = "db lookup failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		if existing != nil {
			extra = CRSMergeMap(existing.Extra, extra)
			credentials = CRSMergeMap(existing.Credentials, credentials)
		}
		ReconcileCRSOllamaCloudUsageExtra(existing, PlatformAnthropic, targetType, credentials, extra)

		if existing == nil {
			if !CRSShouldCreateProvider(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = "not selected"
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			provider := &Record{
				Name:        CRSDefaultName(src.Name, src.ID),
				Platform:    PlatformAnthropic,
				Type:        targetType,
				Credentials: credentials,
				Extra:       CloneValues(extra),
				ProxyID:     proxyID,
				Concurrency: concurrency,
				Priority:    priority,
				Status:      status,
				Schedulable: src.Schedulable,
			}
			if err := s.providerRepo.Create(ctx, provider); err != nil {
				item.Action = "failed"
				item.Error = "create failed: " + err.Error()
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			// 创建成功后尽力刷新令牌。
			if targetType == ProviderTypeOAuth {
				s.refreshOAuthToken(ctx, provider)
			}
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		// 母提供商守卫:CRS ID 跨平台碰撞时,本(Anthropic OAuth)分支不得改坏有 spark 影子的 OpenAI 母提供商。
		if gerr := GuardCRSShadowParentInvariant(ctx, s.providerRepo, existing, PlatformAnthropic, targetType); gerr != nil {
			item.Action = "failed"
			item.Error = gerr.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		// 更新现有提供商配置。
		existing.Extra = CloneValues(extra)
		existing.Name = CRSDefaultName(src.Name, src.ID)
		existing.Platform = PlatformAnthropic
		existing.Type = targetType
		existing.Credentials = credentials
		if proxyID != nil {
			existing.ProxyID = proxyID
		}
		existing.Concurrency = concurrency
		existing.Priority = priority
		existing.Status = status
		existing.Schedulable = src.Schedulable

		if err := WriteConfiguration(ctx, s.providerRepo, existing, CRSSyncConfiguration(proxyID != nil)); err != nil {
			item.Action = "failed"
			item.Error = "update failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		// 配置更新成功后尽力刷新令牌。
		if targetType == ProviderTypeOAuth {
			s.refreshOAuthToken(ctx, existing)
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	// Claude Console 来源转换为 Anthropic API Key。
	for _, src := range exported.Data.ClaudeConsoleProviders {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		apiKey, _ := src.Credentials["api_key"].(string)
		if strings.TrimSpace(apiKey) == "" {
			item.Action = "failed"
			item.Error = "missing api_key"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		proxyID, err := egress.MatchOrCreateCRSProxy(ctx, s.proxyRepo, input.SyncProxies, &proxies, src.Proxy, fmt.Sprintf("crs-%s", src.Name))
		if err != nil {
			item.Action = "failed"
			item.Error = "proxy sync failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := CRSSanitizeCredentialsMap(src.Credentials)
		priority := CRSClampPriority(src.Priority)
		concurrency := 3
		if src.MaxConcurrentTasks > 0 {
			concurrency = src.MaxConcurrentTasks
		}
		status := MapCRSStatus(src.IsActive, src.Status)

		extra := map[string]any{
			"crs_account_id": src.ID,
			"crs_kind":       src.Kind,
			"crs_synced_at":  now,
		}

		existing, err := s.providerRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = "db lookup failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		if existing != nil {
			extra = CRSMergeMap(existing.Extra, extra)
			credentials = CRSMergeMap(existing.Credentials, credentials)
		}
		ReconcileCRSOllamaCloudUsageExtra(existing, PlatformAnthropic, ProviderTypeAPIKey, credentials, extra)

		if existing == nil {
			if !CRSShouldCreateProvider(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = "not selected"
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			provider := &Record{
				Name:        CRSDefaultName(src.Name, src.ID),
				Platform:    PlatformAnthropic,
				Type:        ProviderTypeAPIKey,
				Credentials: credentials,
				Extra:       CloneValues(extra),
				ProxyID:     proxyID,
				Concurrency: concurrency,
				Priority:    priority,
				Status:      status,
				Schedulable: src.Schedulable,
			}
			if err := s.providerRepo.Create(ctx, provider); err != nil {
				item.Action = "failed"
				item.Error = "create failed: " + err.Error()
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		// 母提供商守卫:CRS ID 跨平台碰撞时,本(Anthropic APIKey)分支不得改坏有 spark 影子的 OpenAI 母提供商。
		if gerr := GuardCRSShadowParentInvariant(ctx, s.providerRepo, existing, PlatformAnthropic, ProviderTypeAPIKey); gerr != nil {
			item.Action = "failed"
			item.Error = gerr.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		existing.Extra = CloneValues(extra)
		existing.Name = CRSDefaultName(src.Name, src.ID)
		existing.Platform = PlatformAnthropic
		existing.Type = ProviderTypeAPIKey
		existing.Credentials = credentials
		if proxyID != nil {
			existing.ProxyID = proxyID
		}
		existing.Concurrency = concurrency
		existing.Priority = priority
		existing.Status = status
		existing.Schedulable = src.Schedulable

		if err := WriteConfiguration(ctx, s.providerRepo, existing, CRSSyncConfiguration(proxyID != nil)); err != nil {
			item.Action = "failed"
			item.Error = "update failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	// OpenAI OAuth 来源保留 OAuth 类型。
	for _, src := range exported.Data.OpenAIOAuthProviders {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		accessToken, _ := src.Credentials["access_token"].(string)
		if strings.TrimSpace(accessToken) == "" {
			item.Action = "failed"
			item.Error = "missing access_token"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		proxyID, err := egress.MatchOrCreateCRSProxy(ctx, s.proxyRepo,
			input.SyncProxies,
			&proxies,
			src.Proxy,
			fmt.Sprintf("crs-%s", src.Name),
		)
		if err != nil {
			item.Action = "failed"
			item.Error = "proxy sync failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := CRSSanitizeCredentialsMap(src.Credentials)
		// 规范化令牌类型。
		if v, ok := credentials["token_type"].(string); !ok || strings.TrimSpace(v) == "" {
			credentials["token_type"] = "Bearer"
		}
		// 将 ISO 到期值转换为 Unix 时间戳。
		if expiresAtStr, ok := credentials["expires_at"].(string); ok && expiresAtStr != "" {
			if t, err := time.Parse(time.RFC3339, expiresAtStr); err == nil {
				credentials["expires_at"] = t.Unix()
			}
		}
		priority := CRSClampPriority(src.Priority)
		concurrency := 3
		status := MapCRSStatus(src.IsActive, src.Status)

		// 保留来源扩展字段并补入同步元数据。
		extra := make(map[string]any)
		if src.Extra != nil {
			maps.Copy(extra, src.Extra)
		}
		extra["crs_account_id"] = src.ID
		extra["crs_kind"] = src.Kind
		extra["crs_synced_at"] = now
		// 将来源的 crs_email 写入 email。
		if crsEmail, ok := src.Extra["crs_email"]; ok {
			extra["email"] = crsEmail
		}

		existing, err := s.providerRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = "db lookup failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		var existingExtra map[string]any
		if existing != nil {
			existingExtra = existing.Extra
		}
		extra = CRSMergeMap(existingExtra, extra)
		DiscardDeprecatedExtra(extra)
		if existing != nil {
			credentials = CRSMergeMap(existing.Credentials, credentials)
		}
		ReconcileCRSOllamaCloudUsageExtra(existing, PlatformOpenAI, ProviderTypeOAuth, credentials, extra)

		if existing == nil {
			if !CRSShouldCreateProvider(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = "not selected"
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			provider := &Record{
				Name:        CRSDefaultName(src.Name, src.ID),
				Platform:    PlatformOpenAI,
				Type:        ProviderTypeOAuth,
				Credentials: credentials,
				Extra:       CloneValues(extra),
				ProxyID:     proxyID,
				Concurrency: concurrency,
				Priority:    priority,
				Status:      status,
				Schedulable: src.Schedulable,
			}
			if err := s.providerRepo.Create(ctx, provider); err != nil {
				item.Action = "failed"
				item.Error = "create failed: " + err.Error()
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			// 创建成功后尽力刷新令牌。
			s.refreshOAuthToken(ctx, provider)
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		existing.Extra = CloneValues(extra)
		existing.Name = CRSDefaultName(src.Name, src.ID)
		existing.Platform = PlatformOpenAI
		existing.Type = ProviderTypeOAuth
		existing.Credentials = credentials
		if proxyID != nil {
			existing.ProxyID = proxyID
		}
		existing.Concurrency = concurrency
		existing.Priority = priority
		existing.Status = status
		existing.Schedulable = src.Schedulable

		if err := WriteConfiguration(ctx, s.providerRepo, existing, CRSSyncConfiguration(proxyID != nil)); err != nil {
			item.Action = "failed"
			item.Error = "update failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		// 配置更新成功后尽力刷新令牌。
		s.refreshOAuthToken(ctx, existing)

		// 母提供商的代理更新后同步到 Spark 影子，与管理编辑的继承规则一致。
		// 母提供商更新已提交，影子同步失败时记录告警，该项保持成功。
		if perr := PropagateProviderProxyToShadows(ctx, s.providerRepo, existing.ID, existing.ProxyID); perr != nil {
			s.options.Warn("crs_sync_propagate_proxy_to_shadows_failed", "provider_id", existing.ID, "error", perr)
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	// OpenAI Responses 来源转换为 API Key。
	for _, src := range exported.Data.OpenAIResponsesProviders {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		apiKey, _ := src.Credentials["api_key"].(string)
		if strings.TrimSpace(apiKey) == "" {
			item.Action = "failed"
			item.Error = "missing api_key"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		if baseURL, ok := src.Credentials["base_url"].(string); !ok || strings.TrimSpace(baseURL) == "" {
			src.Credentials["base_url"] = "https://api.openai.com"
		}
		// OpenAI 基地址去除末尾 /v1。
		CRSCleanBaseURL(src.Credentials, "/v1")

		proxyID, err := egress.MatchOrCreateCRSProxy(ctx, s.proxyRepo,
			input.SyncProxies,
			&proxies,
			src.Proxy,
			fmt.Sprintf("crs-%s", src.Name),
		)
		if err != nil {
			item.Action = "failed"
			item.Error = "proxy sync failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := CRSSanitizeCredentialsMap(src.Credentials)
		priority := CRSClampPriority(src.Priority)
		concurrency := 3
		status := MapCRSStatus(src.IsActive, src.Status)

		extra := make(map[string]any, len(src.Extra)+3)
		maps.Copy(extra, src.Extra)
		extra["crs_account_id"] = src.ID
		extra["crs_kind"] = src.Kind
		extra["crs_synced_at"] = now

		existing, err := s.providerRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = "db lookup failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		var existingExtra map[string]any
		if existing != nil {
			existingExtra = existing.Extra
		}
		extra = CRSMergeMap(existingExtra, extra)
		DiscardDeprecatedExtra(extra)
		if existing != nil {
			credentials = CRSMergeMap(existing.Credentials, credentials)
		}
		ReconcileCRSOllamaCloudUsageExtra(existing, PlatformOpenAI, ProviderTypeAPIKey, credentials, extra)

		if existing == nil {
			if !CRSShouldCreateProvider(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = "not selected"
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			provider := &Record{
				Name:        CRSDefaultName(src.Name, src.ID),
				Platform:    PlatformOpenAI,
				Type:        ProviderTypeAPIKey,
				Credentials: credentials,
				Extra:       CloneValues(extra),
				ProxyID:     proxyID,
				Concurrency: concurrency,
				Priority:    priority,
				Status:      status,
				Schedulable: src.Schedulable,
			}
			if err := s.providerRepo.Create(ctx, provider); err != nil {
				item.Action = "failed"
				item.Error = "create failed: " + err.Error()
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		// 有 Spark 影子的母提供商必须保持 OpenAI OAuth；此分支会改成 API Key，必须拒绝更新。
		// 需要先删除影子，才能改变母提供商的凭据类型。
		if gerr := GuardCRSShadowParentInvariant(ctx, s.providerRepo, existing, PlatformOpenAI, ProviderTypeAPIKey); gerr != nil {
			item.Action = "failed"
			item.Error = gerr.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		existing.Extra = CloneValues(extra)
		existing.Name = CRSDefaultName(src.Name, src.ID)
		existing.Platform = PlatformOpenAI
		existing.Type = ProviderTypeAPIKey
		existing.Credentials = credentials
		if proxyID != nil {
			existing.ProxyID = proxyID
		}
		existing.Concurrency = concurrency
		existing.Priority = priority
		existing.Status = status
		existing.Schedulable = src.Schedulable

		if err := WriteConfiguration(ctx, s.providerRepo, existing, CRSSyncConfiguration(proxyID != nil)); err != nil {
			item.Action = "failed"
			item.Error = "update failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	// Gemini OAuth 来源保留 OAuth 类型。
	for _, src := range exported.Data.GeminiOAuthProviders {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		refreshToken, _ := src.Credentials["refresh_token"].(string)
		if strings.TrimSpace(refreshToken) == "" {
			item.Action = "failed"
			item.Error = "missing refresh_token"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		proxyID, err := egress.MatchOrCreateCRSProxy(ctx, s.proxyRepo, input.SyncProxies, &proxies, src.Proxy, fmt.Sprintf("crs-%s", src.Name))
		if err != nil {
			item.Action = "failed"
			item.Error = "proxy sync failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := CRSSanitizeCredentialsMap(src.Credentials)
		if v, ok := credentials["token_type"].(string); !ok || strings.TrimSpace(v) == "" {
			credentials["token_type"] = "Bearer"
		}
		// 按 Gemini 的格式将到期时间转换为 Unix 秒字符串。
		if expiresAtStr, ok := credentials["expires_at"].(string); ok && strings.TrimSpace(expiresAtStr) != "" {
			if t, err := time.Parse(time.RFC3339, expiresAtStr); err == nil {
				credentials["expires_at"] = strconv.FormatInt(t.Unix(), 10)
			}
		}

		extra := make(map[string]any)
		if src.Extra != nil {
			maps.Copy(extra, src.Extra)
		}
		extra["crs_account_id"] = src.ID
		extra["crs_kind"] = src.Kind
		extra["crs_synced_at"] = now

		existing, err := s.providerRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = "db lookup failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		if existing != nil {
			extra = CRSMergeMap(existing.Extra, extra)
			credentials = CRSMergeMap(existing.Credentials, credentials)
		}
		ReconcileCRSOllamaCloudUsageExtra(existing, PlatformGemini, ProviderTypeOAuth, credentials, extra)

		if existing == nil {
			if !CRSShouldCreateProvider(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = "not selected"
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			provider := &Record{
				Name:        CRSDefaultName(src.Name, src.ID),
				Platform:    PlatformGemini,
				Type:        ProviderTypeOAuth,
				Credentials: credentials,
				Extra:       CloneValues(extra),
				ProxyID:     proxyID,
				Concurrency: 3,
				Priority:    CRSClampPriority(src.Priority),
				Status:      MapCRSStatus(src.IsActive, src.Status),
				Schedulable: src.Schedulable,
			}
			if err := s.providerRepo.Create(ctx, provider); err != nil {
				item.Action = "failed"
				item.Error = "create failed: " + err.Error()
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			s.refreshOAuthToken(ctx, provider)
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		// 母提供商守卫:CRS ID 跨平台碰撞时,本(Gemini OAuth)分支不得改坏有 spark 影子的 OpenAI 母提供商。
		if gerr := GuardCRSShadowParentInvariant(ctx, s.providerRepo, existing, PlatformGemini, ProviderTypeOAuth); gerr != nil {
			item.Action = "failed"
			item.Error = gerr.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		existing.Extra = CloneValues(extra)
		existing.Name = CRSDefaultName(src.Name, src.ID)
		existing.Platform = PlatformGemini
		existing.Type = ProviderTypeOAuth
		existing.Credentials = credentials
		if proxyID != nil {
			existing.ProxyID = proxyID
		}
		existing.Concurrency = 3
		existing.Priority = CRSClampPriority(src.Priority)
		existing.Status = MapCRSStatus(src.IsActive, src.Status)
		existing.Schedulable = src.Schedulable

		if err := WriteConfiguration(ctx, s.providerRepo, existing, CRSSyncConfiguration(proxyID != nil)); err != nil {
			item.Action = "failed"
			item.Error = "update failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		s.refreshOAuthToken(ctx, existing)

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	// Gemini API Key 来源保留 API Key 类型。
	for _, src := range exported.Data.GeminiAPIKeyProviders {
		item := SyncFromCRSItemResult{
			CRSAccountID: src.ID,
			Kind:         src.Kind,
			Name:         src.Name,
		}

		apiKey, _ := src.Credentials["api_key"].(string)
		if strings.TrimSpace(apiKey) == "" {
			item.Action = "failed"
			item.Error = "missing api_key"
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		proxyID, err := egress.MatchOrCreateCRSProxy(ctx, s.proxyRepo, input.SyncProxies, &proxies, src.Proxy, fmt.Sprintf("crs-%s", src.Name))
		if err != nil {
			item.Action = "failed"
			item.Error = "proxy sync failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		credentials := CRSSanitizeCredentialsMap(src.Credentials)
		if baseURL, ok := credentials["base_url"].(string); !ok || strings.TrimSpace(baseURL) == "" {
			credentials["base_url"] = "https://generativelanguage.googleapis.com"
		}

		extra := make(map[string]any)
		if src.Extra != nil {
			maps.Copy(extra, src.Extra)
		}
		extra["crs_account_id"] = src.ID
		extra["crs_kind"] = src.Kind
		extra["crs_synced_at"] = now

		existing, err := s.providerRepo.GetByCRSAccountID(ctx, src.ID)
		if err != nil {
			item.Action = "failed"
			item.Error = "db lookup failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}
		if existing != nil {
			extra = CRSMergeMap(existing.Extra, extra)
			credentials = CRSMergeMap(existing.Credentials, credentials)
		}
		ReconcileCRSOllamaCloudUsageExtra(existing, PlatformGemini, ProviderTypeAPIKey, credentials, extra)

		if existing == nil {
			if !CRSShouldCreateProvider(src.ID, selectedSet) {
				item.Action = "skipped"
				item.Error = "not selected"
				result.Skipped++
				result.Items = append(result.Items, item)
				continue
			}
			provider := &Record{
				Name:        CRSDefaultName(src.Name, src.ID),
				Platform:    PlatformGemini,
				Type:        ProviderTypeAPIKey,
				Credentials: credentials,
				Extra:       CloneValues(extra),
				ProxyID:     proxyID,
				Concurrency: 3,
				Priority:    CRSClampPriority(src.Priority),
				Status:      MapCRSStatus(src.IsActive, src.Status),
				Schedulable: src.Schedulable,
			}
			if err := s.providerRepo.Create(ctx, provider); err != nil {
				item.Action = "failed"
				item.Error = "create failed: " + err.Error()
				result.Failed++
				result.Items = append(result.Items, item)
				continue
			}
			item.Action = "created"
			result.Created++
			result.Items = append(result.Items, item)
			continue
		}

		// 母提供商守卫:CRS ID 跨平台碰撞时,本(Gemini APIKey)分支不得改坏有 spark 影子的 OpenAI 母提供商。
		if gerr := GuardCRSShadowParentInvariant(ctx, s.providerRepo, existing, PlatformGemini, ProviderTypeAPIKey); gerr != nil {
			item.Action = "failed"
			item.Error = gerr.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		existing.Extra = CloneValues(extra)
		existing.Name = CRSDefaultName(src.Name, src.ID)
		existing.Platform = PlatformGemini
		existing.Type = ProviderTypeAPIKey
		existing.Credentials = credentials
		if proxyID != nil {
			existing.ProxyID = proxyID
		}
		existing.Concurrency = 3
		existing.Priority = CRSClampPriority(src.Priority)
		existing.Status = MapCRSStatus(src.IsActive, src.Status)
		existing.Schedulable = src.Schedulable

		if err := WriteConfiguration(ctx, s.providerRepo, existing, CRSSyncConfiguration(proxyID != nil)); err != nil {
			item.Action = "failed"
			item.Error = "update failed: " + err.Error()
			result.Failed++
			result.Items = append(result.Items, item)
			continue
		}

		item.Action = "updated"
		result.Updated++
		result.Items = append(result.Items, item)
	}

	return result, nil
}

func CRSMergeMap(existing map[string]any, updates map[string]any) map[string]any {
	out := make(map[string]any, len(existing)+len(updates))
	maps.Copy(out, existing)
	maps.Copy(out, updates)
	return CloneValues(out)
}

func ReconcileCRSOllamaCloudUsageExtra(
	existing *Record,
	targetPlatform, targetType string,
	targetCredentials map[string]any,
	extra map[string]any,
) {
	DiscardDeprecatedExtra(extra)
	for _, key := range []string{
		OllamaCloudUsageSessionExtraKey,
		OllamaCloudUsageAutoRefreshExtraKey,
		OllamaCloudUsageSnapshotExtraKey,
	} {
		delete(extra, key)
	}
	if existing == nil {
		return
	}
	target := &Record{Platform: targetPlatform, Type: targetType, Credentials: targetCredentials}
	if IsOllamaCloudUsageProvider(existing) && IsOllamaCloudUsageProvider(target) &&
		reflect.DeepEqual(OllamaCloudUsageIdentity(existing), OllamaCloudUsageIdentity(target)) {
		if session, ok := existing.Extra[OllamaCloudUsageSessionExtraKey]; ok {
			extra[OllamaCloudUsageSessionExtraKey] = session
		}
		if enabled, ok := existing.Extra[OllamaCloudUsageAutoRefreshExtraKey]; ok {
			extra[OllamaCloudUsageAutoRefreshExtraKey] = enabled
		}
		if snapshot, ok := existing.Extra[OllamaCloudUsageSnapshotExtraKey]; ok {
			extra[OllamaCloudUsageSnapshotExtraKey] = snapshot
		}
	}
}

func CRSDefaultName(name, id string) string {
	if strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return "CRS " + id
}

func CRSClampPriority(priority int) int {
	if priority < 1 || priority > 100 {
		return 50
	}
	return priority
}

func CRSSanitizeCredentialsMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		// 按原约定省略 nil 凭据键。
		if v != nil {
			out[k] = v
		}
	}
	return CloneValues(out)
}

func MapCRSStatus(isActive bool, status string) string {
	if !isActive {
		return "inactive"
	}
	if strings.EqualFold(strings.TrimSpace(status), "error") {
		return "error"
	}
	return "active"
}

// CRSCleanBaseURL 仅删除 Claude/OpenAI 基地址的既定末尾段。
func CRSCleanBaseURL(credentials map[string]any, suffixToRemove string) {
	if baseURL, ok := credentials["base_url"].(string); ok && baseURL != "" {
		trimmed := strings.TrimSpace(baseURL)
		if before, ok0 := strings.CutSuffix(trimmed, suffixToRemove); ok0 {
			credentials["base_url"] = before
		}
	}
}

// CRSBuildSelectedSet 在未发送集合时选择全部条目，空集合表示新建数量为零。
func CRSBuildSelectedSet(ids []string) map[string]struct{} {
	if ids == nil {
		return nil
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

// CRSShouldCreateProvider 仅决定是否创建，未选中不阻止更新已存在提供商。
func CRSShouldCreateProvider(crsID string, selectedSet map[string]struct{}) bool {
	if selectedSet == nil {
		return true
	}
	_, ok := selectedSet[crsID]
	return ok
}

// PreviewFromCRS 先获取导出，再一次查询本地来源 ID，保持原分类顺序。
func (s *CRSSync) PreviewFromCRS(ctx context.Context, input SyncFromCRSInput) (*PreviewFromCRSResult, error) {
	exported, err := s.exporter.Fetch(ctx, input.BaseURL, input.Username, input.Password)
	if err != nil {
		return nil, err
	}

	// 一次读取已存在的来源 ID。
	existingCRSIDs, err := s.providerRepo.ListCRSAccountIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list existing CRS providers: %w", err)
	}

	result := &PreviewFromCRSResult{
		NewProviders:      make([]CRSPreviewProvider, 0),
		ExistingProviders: make([]CRSPreviewProvider, 0),
	}

	classify := func(crsID, kind, name, platform, providerType string) {
		preview := CRSPreviewProvider{
			CRSAccountID: crsID,
			Kind:         kind,
			Name:         CRSDefaultName(name, crsID),
			Platform:     platform,
			Type:         providerType,
		}
		if _, exists := existingCRSIDs[crsID]; exists {
			result.ExistingProviders = append(result.ExistingProviders, preview)
		} else {
			result.NewProviders = append(result.NewProviders, preview)
		}
	}

	for _, src := range exported.Data.ClaudeProviders {
		authType := strings.TrimSpace(src.AuthType)
		if authType == "" {
			authType = ProviderTypeOAuth
		}
		classify(src.ID, src.Kind, src.Name, PlatformAnthropic, authType)
	}
	for _, src := range exported.Data.ClaudeConsoleProviders {
		classify(src.ID, src.Kind, src.Name, PlatformAnthropic, ProviderTypeAPIKey)
	}
	for _, src := range exported.Data.OpenAIOAuthProviders {
		classify(src.ID, src.Kind, src.Name, PlatformOpenAI, ProviderTypeOAuth)
	}
	for _, src := range exported.Data.OpenAIResponsesProviders {
		classify(src.ID, src.Kind, src.Name, PlatformOpenAI, ProviderTypeAPIKey)
	}
	for _, src := range exported.Data.GeminiOAuthProviders {
		classify(src.ID, src.Kind, src.Name, PlatformGemini, ProviderTypeOAuth)
	}
	for _, src := range exported.Data.GeminiAPIKeyProviders {
		classify(src.ID, src.Kind, src.Name, PlatformGemini, ProviderTypeAPIKey)
	}

	return result, nil
}
