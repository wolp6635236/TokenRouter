package dto

import (
	"encoding/json"
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/site/content"
)

// FooterLinkGroup 首页底栏链接分组（一列）。
type FooterLinkGroup = content.FooterLinkGroup

// FooterLink 首页底栏单条链接。
type FooterLink = content.FooterLink

// CustomEndpoint represents an admin-configured API endpoint for quick copy.
type CustomEndpoint = content.CustomEndpoint

// CustomMenuItem represents a user-configured custom menu entry.
type CustomMenuItem = content.CustomMenuItem

// ParseHomeFeaturedModels 将 JSON 字符串解析为首页展示模型 ID 列表。
// 空串或非法输入返回空切片。
func ParseHomeFeaturedModels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return []string{}
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		return []string{}
	}
	return models
}

// ParseFooterLinks parses a JSON string into a slice of FooterLinkGroup.
// Returns empty slice on empty/invalid input.
func ParseFooterLinks(raw string) []FooterLinkGroup {
	return content.ParseFooterGroups(raw)
}

// ParseCustomEndpoints parses a JSON string into a slice of CustomEndpoint.
// Returns empty slice on empty/invalid input.
func ParseCustomEndpoints(raw string) []CustomEndpoint {
	return content.ParseEndpoints(raw)
}

// ParseCustomMenuItems parses a JSON string into a slice of CustomMenuItem.
// Returns empty slice on empty/invalid input.
func ParseCustomMenuItems(raw string) []CustomMenuItem {
	return content.ParseMenus(raw)
}

// ParseUserVisibleMenuItems parses custom menu items and filters out admin-only entries.
func ParseUserVisibleMenuItems(raw string) []CustomMenuItem {
	items := ParseCustomMenuItems(raw)
	filtered := make([]CustomMenuItem, 0, len(items))
	for _, item := range items {
		if item.Visibility != "admin" {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
