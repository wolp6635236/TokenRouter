package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
)

// TooLargeWriter 在原 HTTP 入口写出对应协议的超限错误。
type TooLargeWriter func(*gin.Context)

// ReadUpstreamResponseBody 读取上游响应体，并记录 Ops 和协议错误。
func ReadUpstreamResponseBody(reader io.Reader, maxBytes int64, c *gin.Context, onTooLarge TooLargeWriter) ([]byte, error) {
	body, err := httpclient.ReadResponseBodyLimited(reader, maxBytes)
	if err != nil {
		if errors.Is(err, httpclient.ErrResponseBodyTooLarge) {
			SetOpsUpstreamError(c, http.StatusBadGateway, "upstream response too large", "")
			if onTooLarge != nil {
				onTooLarge(c)
			}
		}
		return nil, err
	}
	return body, nil
}

func AnthropicResponseTooLarge(c *gin.Context) {
	c.JSON(http.StatusBadGateway, gin.H{"type": "error", "error": gin.H{"type": "upstream_error", "message": "Upstream response too large"}})
}

func OpenAIResponseTooLarge(c *gin.Context) {
	c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"type": "upstream_error", "message": "Upstream response too large"}})
}
