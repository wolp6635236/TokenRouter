package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
)

// NavigationCopy 是用户菜单和页脚链接可翻译的展示内容。
type NavigationCopy struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// CustomMenuItem 按稳定 ID 保存菜单，页面授权使用 PageSlug 和 Visibility。
type CustomMenuItem struct {
	Resolution   *locale.Resolution             `json:"localization_resolution,omitempty"`
	ID           string                         `json:"id"`
	Label        string                         `json:"label"`
	IconSVG      string                         `json:"icon_svg"`
	URL          string                         `json:"url"`
	PageSlug     string                         `json:"page_slug,omitempty"`
	Visibility   string                         `json:"visibility"`
	SortOrder    int                            `json:"sort_order"`
	Localization *locale.Update[NavigationCopy] `json:"localization,omitempty"`
}

// EndpointCopy 包含接口复制入口的名称和说明。
type EndpointCopy struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CustomEndpoint 的接口地址在各语言间共用。
type CustomEndpoint struct {
	Resolution   *locale.Resolution           `json:"localization_resolution,omitempty"`
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	Endpoint     string                       `json:"endpoint"`
	Description  string                       `json:"description"`
	Localization *locale.Update[EndpointCopy] `json:"localization,omitempty"`
}

// FooterLinkGroup 包含一列页脚链接，标题单独翻译。
type FooterLinkGroup struct {
	Resolution   *locale.Resolution     `json:"localization_resolution,omitempty"`
	ID           string                 `json:"id"`
	Title        string                 `json:"title"`
	Links        []FooterLink           `json:"links"`
	Localization *locale.Update[string] `json:"localization,omitempty"`
}

// FooterLink 通过稳定 ID 对应各语言的文字和目标地址。
type FooterLink struct {
	Resolution   *locale.Resolution             `json:"localization_resolution,omitempty"`
	ID           string                         `json:"id"`
	Label        string                         `json:"label"`
	URL          string                         `json:"url"`
	Localization *locale.Update[NavigationCopy] `json:"localization,omitempty"`
}

// legacyItemID 给尚未保存 ID 的历史条目生成确定的编辑标识。
func legacyItemID(kind string, index int, value string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", kind, index, value)))
	return hex.EncodeToString(sum[:8])
}

// ParseMenus 保留历史菜单 ID；管理员条目使用单语言资料。
func ParseMenus(raw string) []CustomMenuItem {
	items := []CustomMenuItem{}
	_ = json.Unmarshal([]byte(raw), &items)
	for i := range items {
		if items[i].ID == "" {
			items[i].ID = legacyItemID("menu", i, items[i].Label)
		}
	}
	return items
}

// ParseEndpoints 将接口配置解析为带稳定 ID 的条目。
func ParseEndpoints(raw string) []CustomEndpoint {
	items := []CustomEndpoint{}
	_ = json.Unmarshal([]byte(raw), &items)
	for i := range items {
		if items[i].ID == "" {
			items[i].ID = legacyItemID("endpoint", i, items[i].Endpoint)
		}
	}
	return items
}

// ParseFooterGroups 为历史分组和链接补充编辑标识。
func ParseFooterGroups(raw string) []FooterLinkGroup {
	items := []FooterLinkGroup{}
	_ = json.Unmarshal([]byte(raw), &items)
	for i := range items {
		if items[i].ID == "" {
			items[i].ID = legacyItemID("footer", i, items[i].Title)
		}
		for j := range items[i].Links {
			if items[i].Links[j].ID == "" {
				items[i].Links[j].ID = legacyItemID(items[i].ID, j, items[i].Links[j].URL)
			}
		}
	}
	return items
}
