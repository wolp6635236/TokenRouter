package admission

import (
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
)

// GroupAllowed 检查用户是否有权绑定 Key 所属分组，Key、用户或分组信息缺失时返回 true。
func GroupAllowed(key *apikey.APIKey) bool {
	if key == nil || key.GroupID == nil || key.User == nil || key.Group == nil {
		return true
	}
	return key.User.CanBindGroup(key.Group.ID, key.Group.IsExclusive)
}

// GroupAvailable 区分删除与停用，公开错误 reason 保持不变。
func GroupAvailable(key *apikey.APIKey) (string, string, bool) {
	if key == nil || key.GroupID == nil {
		return "", "", true
	}
	group := key.Group
	if group == nil || strings.EqualFold(group.Status, "deleted") {
		return "GROUP_DELETED", "The API key group has been deleted.", false
	}
	if !group.IsActive() {
		return "GROUP_DISABLED", "The API key group is disabled.", false
	}
	return "", "", true
}
