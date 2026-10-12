package pagination

import (
	"strings"
)

const (
	// SortOrderAsc 表示升序排列。
	SortOrderAsc = "asc"

	// SortOrderDesc 表示降序排列。
	SortOrderDesc = "desc"
)

// PaginationParams 保存页码、每页数量和排序设置。
type PaginationParams struct {
	Page      int
	PageSize  int
	SortBy    string
	SortOrder string
}

// PaginationResult 保存总条数和分页信息。
type PaginationResult struct {
	Total    int64
	Page     int
	PageSize int
	Pages    int
}

// Offset 返回当前页在结果集中的起始位置。
func (p PaginationParams) Offset() int {
	if p.Page < 1 {
		p.Page = 1
	}
	return (p.Page - 1) * p.Limit()
}

// Limit 返回每页数量，默认 20 条，上限 1000 条。
func (p PaginationParams) Limit() int {
	if p.PageSize < 1 {
		return 20
	}
	if p.PageSize > 1000 {
		return 1000
	}
	return p.PageSize
}

// NormalizeSortOrder 将排序方向转换为 asc 或 desc，无效输入使用默认方向。
func NormalizeSortOrder(order string, defaultOrder string) string {
	switch strings.ToLower(strings.TrimSpace(defaultOrder)) {
	case SortOrderAsc:
		defaultOrder = SortOrderAsc
	default:
		defaultOrder = SortOrderDesc
	}

	switch strings.ToLower(strings.TrimSpace(order)) {
	case SortOrderAsc:
		return SortOrderAsc
	case SortOrderDesc:
		return SortOrderDesc
	default:
		return defaultOrder
	}
}

// NormalizedSortOrder 返回参数中的排序方向，无效输入使用默认方向。
func (p PaginationParams) NormalizedSortOrder(defaultOrder string) string {
	return NormalizeSortOrder(p.SortOrder, defaultOrder)
}

// ResultFromTotal 按总条数和分页参数计算总页数。
func ResultFromTotal(total int64, params PaginationParams) *PaginationResult {
	pages := int(total) / params.Limit()
	if int(total)%params.Limit() > 0 {
		pages++
	}
	return &PaginationResult{
		Total:    total,
		Page:     params.Page,
		PageSize: params.Limit(),
		Pages:    pages,
	}
}

// Slice 根据分页参数返回当前页的切片。
func Slice[T any](items []T, params PaginationParams) []T {
	if len(items) == 0 {
		return []T{}
	}

	offset := params.Offset()
	if offset >= len(items) {
		return []T{}
	}

	limit := params.Limit()
	end := min(offset+limit, len(items))

	return items[offset:end]
}
