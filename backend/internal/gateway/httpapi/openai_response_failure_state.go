package httpapi

import (
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
)

const responseFailureEffectsKey = "gateway_response_failure_effects"

var responseFailureEffectsInit sync.Mutex

// responseFailureEffects 返回当前请求登记的状态，未登记时返回空值。
func responseFailureEffects(c *gin.Context, create bool) *requeststate.ResponseFailureEffects {
	if c == nil {
		return nil
	}
	if v, ok := c.Get(responseFailureEffectsKey); ok {
		if s, ok := v.(*requeststate.ResponseFailureEffects); ok {
			return s
		}
	}
	if !create {
		return nil
	}
	responseFailureEffectsInit.Lock()
	defer responseFailureEffectsInit.Unlock()
	if v, ok := c.Get(responseFailureEffectsKey); ok {
		if s, ok := v.(*requeststate.ResponseFailureEffects); ok {
			return s
		}
	}
	s := &requeststate.ResponseFailureEffects{}
	c.Set(responseFailureEffectsKey, s)
	return s
}

func MarkOpenAIResponseFailureEffects(c *gin.Context, status int, disabled bool) {
	if c != nil {
		responseFailureEffects(c, true).Store(status, disabled)
	}
}

func consumeOpenAIResponseFailureEffects(c *gin.Context) (int, bool, bool) {
	return responseFailureEffects(c, false).Consume()
}
