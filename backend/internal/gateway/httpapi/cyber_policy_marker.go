package httpapi

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/gateway/moderationflow"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

// opsCyberPolicyKey 是 HTTP 和 WS turn 记录供应商拒绝证据的键。
const opsCyberPolicyKey = "ops_cyber_policy"

// MarkOpsCyberPolicy 保持首个标记生效，后续事件不覆盖原用量与失败状态。
func MarkOpsCyberPolicy(c *gin.Context, mark moderationflow.Mark) {
	if c == nil || GetOpsCyberPolicy(c) != nil {
		return
	}
	mark.Code = "cyber_policy"
	mark.Message = strings.TrimSpace(mark.Message)
	mark.Body = strings.TrimSpace(mark.Body)
	c.Set(opsCyberPolicyKey, &mark)
}

// GetOpsCyberPolicy 返回当前请求或 turn 的既有标记，不补造成功或结算资格。
func GetOpsCyberPolicy(c *gin.Context) *moderationflow.Mark {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(opsCyberPolicyKey); ok {
		if mark, ok := value.(*moderationflow.Mark); ok && mark != nil {
			return mark
		}
	}
	return nil
}

// ClearOpsCyberPolicy 保持原带类型 nil 清理行为，防止下一个 WS turn 继承旧标记。
func ClearOpsCyberPolicy(c *gin.Context) {
	if c != nil {
		c.Set(opsCyberPolicyKey, (*moderationflow.Mark)(nil))
	}
}

// MarkOpenAICyberPolicyEvent 由平台适配解析原生事件，HTTP 只保存观测值。
func MarkOpenAICyberPolicyEvent(c *gin.Context, payload []byte, upstreamStatus int, usage *openai.ForwardUsage) bool {
	mark := gatewayadapter.ParseOpenAICyberPolicyEvent(payload, upstreamStatus, usage)
	if mark == nil {
		return false
	}
	MarkOpsCyberPolicy(c, *mark)
	return true
}
