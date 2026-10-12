package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	forwardcore "github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
)

func (h OpenAIErrorOutput) EnsureResponse(c *gin.Context, streamStarted bool, err error) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	// 两类心跳停止后读取 Writer 状态，此时心跳 goroutine 已结束写入。
	compactKeepaliveCommitted := StopOpenAICompactSSEKeepaliveCommitted(c)
	if compactKeepaliveCommitted {
		streamStarted = true
	}
	imageKeepalivePresent := OpenAIImagesJSONKeepalivePresent(c)
	StopOpenAIImagesJSONKeepaliveCommitted(c)
	imageKeepalivePaddingOnly := false
	imageKeepaliveResponseWritten := false
	if imageKeepalivePresent {
		adjustedSize := OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c)
		imageKeepalivePaddingOnly = adjustedSize < 0
		imageKeepaliveResponseWritten = adjustedSize >= 0
	}
	compactKeepaliveHasMeaningfulOutput := compactKeepaliveCommitted && OpenAICompactKeepaliveAdjustedWrittenSize(c) > 0
	// Compact 心跳提交 200 响应头后，如果尚未写出协议事件，补充 response.failed。
	if (IsResponseCommitted(c) && (!compactKeepaliveCommitted || compactKeepaliveHasMeaningfulOutput)) ||
		(!compactKeepaliveCommitted && imageKeepaliveResponseWritten) {
		return false
	}
	errType := "upstream_error"
	message := "Upstream request failed"
	status := http.StatusBadGateway
	if warning, ok := forwardcore.WarningFromError(err); ok && gatewayprovider.IsOpenAICyberWarningPayload(warning.ResponseBody, warning.Message) {
		errType = "invalid_request_error"
		message = gatewayprovider.ExtractOpenAICyberWarningMessage(warning.ResponseBody, warning.Message)
		if warning.StatusCode >= 400 && warning.StatusCode <= 599 {
			status = warning.StatusCode
		}
	}
	// 普通 SSE 心跳写出后，追加协议终态。图片 JSON 若仅写出心跳空白，则补写 JSON 错误。
	if c.Writer.Written() && !imageKeepalivePaddingOnly {
		streamStarted = true
	}
	h.StreamError(c, status, errType, message, streamStarted)
	return true
}

func ShouldLogOpenAIForwardFailureAsWarn(c *gin.Context, wroteFallback bool) bool {
	if wroteFallback {
		return false
	}
	if c == nil || c.Writer == nil {
		return false
	}
	return c.Writer.Written()
}

// OpenAIForwardErrorAlreadyCommunicated 判断转发层是否已把上游终止错误写给客户端。
//
// 响应流可能收到状态码 200 里的终止失败事件，例如安全策略拒绝。
// 转发层会先原样转发该终止事件，再返回错误给处理层做日志和统计；
// 处理层不能再追加通用失败事件，否则严格客户端会看到重复终止事件。
func OpenAIForwardErrorAlreadyCommunicated(c *gin.Context, writerSizeBeforeForward int, err error) bool {
	if err == nil || c == nil || c.Writer == nil {
		return false
	}
	// 扣除 compact 心跳字节后判断响应是否开始输出（#3887）。
	if OpenAICompactKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward || OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward {
		return false
	}
	if GetOpsCyberPolicy(c) != nil {
		return true
	}

	msg := strings.TrimSpace(err.Error())
	for _, prefix := range []string{
		"upstream response failed:",
		"non-streaming openai protocol error:",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}
