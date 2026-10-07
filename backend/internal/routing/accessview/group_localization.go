package accessview

import "github.com/TokenFlux/TokenRouter/internal/pkg/locale"

// GroupCopy 保存面向用户的展示名称和描述。
type GroupCopy struct {
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

// GroupLocalization 与分组的业务名称分别持久化。
type GroupLocalization locale.Content[GroupCopy]
