package routing

import (
	"context"

	infraerrors "github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
)

var (
	ErrGroupNotFound = infraerrors.NotFound("GROUP_NOT_FOUND", "group not found")
	ErrGroupExists   = infraerrors.Conflict("GROUP_EXISTS", "group name already exists")
)

type GroupRepository interface {
	Create(ctx context.Context, group *Group) error
	GetByID(ctx context.Context, id int64) (*Group, error)
	GetByIDLite(ctx context.Context, id int64) (*Group, error)
	Update(ctx context.Context, group *Group) error
	Delete(ctx context.Context, id int64) error
	DeleteCascade(ctx context.Context, id int64) ([]int64, error)

	List(ctx context.Context, params pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]Group, *pagination.PaginationResult, error)
	ListActive(ctx context.Context) ([]Group, error)

	ExistsByName(ctx context.Context, name string) (bool, error)
	GetProviderCount(ctx context.Context, groupID int64) (total int64, active int64, err error)
	DeleteProviderGroupsByGroupID(ctx context.Context, groupID int64) (int64, error)
	// GetProviderIDsByGroupIDs 获取多个分组的所有提供商 ID（去重）
	GetProviderIDsByGroupIDs(ctx context.Context, groupIDs []int64) ([]int64, error)
	// BindProvidersToGroup 将多个提供商绑定到指定分组
	BindProvidersToGroup(ctx context.Context, groupID int64, providerIDs []int64) error
	// UpdateSortOrders 批量更新分组排序
	UpdateSortOrders(ctx context.Context, updates []GroupSortOrderUpdate) error
}

type GroupDuplicateRepository interface {
	// FindByDuplicateOperationID 在幂等存储结果不明确时执行只读恢复查询。
	FindByDuplicateOperationID(ctx context.Context, operationID string) (*Group, error)
	// CreateFromSource 原子保存分组、源分组提供商优先级和调度 outbox 事件。
	CreateFromSource(ctx context.Context, group *Group, sourceGroupID int64) error
}

// GroupSortOrderRepository 串行化新建分组的末尾排序位置分配。
type GroupSortOrderRepository interface {
	LockGroupSortOrder(ctx context.Context) error
}

// GroupSortOrderUpdate 分组排序更新。
type GroupSortOrderUpdate struct {
	ID        int64 `json:"id"`
	SortOrder int   `json:"sort_order"`
}
