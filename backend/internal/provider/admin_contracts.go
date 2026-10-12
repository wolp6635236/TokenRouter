package provider

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
)

// AdminStore 提供管理用例所需的读取、状态写入和删除操作。
type AdminStore interface {
	UpdateExtra(context.Context, int64, map[string]any) error
	BulkUpdate(context.Context, []int64, ProviderBulkUpdate) (int64, error)
	RevertProxyFallback(context.Context, int64) error
	Update(context.Context, *Record) error
	FindByExtraField(context.Context, string, any) ([]Record, error)
	Create(context.Context, *Record) error
	BindGroups(context.Context, int64, []int64) error
	ListByGroup(context.Context, int64) ([]Record, error)
	GetByID(context.Context, int64) (*Record, error)
	GetByIDs(context.Context, []int64) ([]*Record, error)
	ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, string) ([]Record, *pagination.PaginationResult, error)
	ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]Record, error)
	ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]Record, error)
	ListSchedulableUngroupedByPlatforms(context.Context, []string) ([]Record, error)
	ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Record, error)
	ListSchedulableUngroupedByPlatform(context.Context, string) ([]Record, error)
	ListShadowsByParent(context.Context, int64) ([]*Record, error)
	Delete(context.Context, int64) error
	ClearError(context.Context, int64) error
	ClearRateLimit(context.Context, int64) error
	ClearAntigravityQuotaScopes(context.Context, int64) error
	ClearModelRateLimits(context.Context, int64) error
	ClearTempUnschedulable(context.Context, int64) error
	SetError(context.Context, int64, string) error
	SetSchedulable(context.Context, int64, bool) error
}

type ProviderQuotaResetter interface {
	ResetQuotaUsedAndClearRateLimitCooldown(context.Context, int64) error
}

type (
	RuntimeUnblocker interface{ ClearProviderSchedulingBlock(int64) }
	AdminOptions     struct {
		ShadowModels   func() map[string]any
		Duplicates     DuplicateStore
		Groups         AdminGroups
		Proxies        PrivacyProxyReader
		Creation       CreationOptions
		Credentials    CreateCredentialHooks
		Background     func(string, func()) bool
		Error          func(string, ...any)
		Privacy        *PrivacyService
		Quotas         ProviderQuotaResetter
		RuntimeBlocker RuntimeUnblocker
	}
)

// Admin 拥有提供商管理规则，资金操作和运行时阻断通过消费者侧接口注入。
type Admin struct {
	providerRepo AdminStore
	options      AdminOptions
}

type GroupReference struct {
	RequireOAuthOnly bool
	ID               int64
	Name             string
}

type AdminGroups interface {
	ActiveGroups(context.Context, string) ([]GroupReference, error)
	ValidateGroups(context.Context, []int64) error
	GetGroup(context.Context, int64) (*GroupReference, error)
}

type CreateCredentialHooks struct {
	// 平台接口按 Qoder 站点切换和 PAT 校验的顺序执行。
	Site         func(*Record) (string, error)
	ValidateEdit func(context.Context, *Record, bool) error
	Prepare      func(*Record)
	Validate     func(context.Context, *Record) error
}

// DuplicateStore 拥有原提供商与关联一次提交，不执行平台交换。
type DuplicateStore interface {
	CreateWithProviderGroups(context.Context, *Record, []GroupMembership) error
}

// ShadowProxyStore 按顺序将母提供商的代理配置同步给影子。
type ShadowProxyStore interface {
	ListShadowsByParent(context.Context, int64) ([]*Record, error)
	Update(context.Context, *Record) error
}

func NewAdmin(store AdminStore, options AdminOptions) *Admin {
	return &Admin{providerRepo: store, options: options}
}
