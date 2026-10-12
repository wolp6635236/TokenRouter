package httpapi

import (
	"strings"

	"github.com/gin-gonic/gin"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	rawwire "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
)

const (
	upstreamResponseModelObserverContextKey = "upstream_response_model_observer"
)

func BeginUpstreamResponseModelObservation(c *gin.Context) *forwardcore.ResponseObserver {
	observer := &forwardcore.ResponseObserver{}
	if c != nil {
		c.Set(upstreamResponseModelObserverContextKey, observer)
	}
	return observer
}

func UpstreamResponseModelObserverFromContext(c *gin.Context) *forwardcore.ResponseObserver {
	if c == nil {
		return nil
	}
	value, ok := c.Get(upstreamResponseModelObserverContextKey)
	if !ok {
		return nil
	}
	observer, _ := value.(*forwardcore.ResponseObserver)
	return observer
}

func ObservedUpstreamResponseServiceTier(c *gin.Context) string {
	return UpstreamResponseModelObserverFromContext(c).ServiceTier()
}

// ResolvedOpenAIUpstreamServiceTierFromObserver 保留最终出站档位；实际响应
// 档位在用量记录时结合凭据协议处理，不在观测阶段提升请求档位。
func ResolvedOpenAIUpstreamServiceTierFromObserver(_ *forwardcore.ResponseObserver, outboundBodyTier *string) *string {
	return outboundBodyTier
}

func ResolvedOpenAIUpstreamServiceTier(c *gin.Context, outboundBodyTier *string) *string {
	return ResolvedOpenAIUpstreamServiceTierFromObserver(UpstreamResponseModelObserverFromContext(c), outboundBodyTier)
}

// ObserveOpenAIServiceTierInContext 将原始 OpenAI 响应事件写入当前请求的
// observer，供同步完成快照保存模型和服务档位。
func ObserveOpenAIServiceTierInContext(c *gin.Context, payload []byte, eventType string) {
	if c == nil || len(payload) == 0 {
		return
	}
	observer := UpstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = BeginUpstreamResponseModelObservation(c)
	}
	observer.ObserveOpenAI(payload, eventType)
}

// ObserveOpenAISSEBody 逐帧记录 Responses SSE 中的实际服务档位。
func ObserveOpenAISSEBody(c *gin.Context, body string) {
	if c == nil || strings.TrimSpace(body) == "" {
		return
	}
	rawwire.ForEachOpenAISSEFrame(body, func(eventType string, payload []byte) {
		ObserveOpenAIServiceTierInContext(c, payload, eventType)
	})
}

// captureResponseModel 在单次执行返回前固定原始观测，原生执行器的结果优先。
func captureResponseModel(c *gin.Context, result *forwardcore.OpenAIResult) {
	if result != nil && result.UpstreamResponseModel == "" {
		result.UpstreamResponseModel = UpstreamResponseModelObserverFromContext(c).Model()
	}
}

// resetResponseModel 清除上一尝试的模型，恢复请求从自身响应读取模型。
func resetResponseModel(c *gin.Context) {
	if observer := UpstreamResponseModelObserverFromContext(c); observer != nil {
		observer.ResetModel()
	} else {
		BeginUpstreamResponseModelObservation(c)
	}
}
