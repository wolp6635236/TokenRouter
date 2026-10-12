package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

func (p *OpenAIResponseOutput) BufferedReadFailure(
	c *gin.Context,
	provider *gatewayprovider.ExecutionProvider,
	resp *http.Response,
	requestID string,
	err error,
) error {
	var readErr *openai.CompatBufferedReadError
	if !errors.As(err, &readErr) || readErr == nil || errors.Is(readErr.Unwrap(), bufio.ErrTooLong) {
		return err
	}
	var requestContext context.Context
	if c != nil && c.Request != nil {
		requestContext = c.Request.Context()
	}
	if !openai.ShouldClassifyUpstreamStreamReadError(readErr.Unwrap(), httpclient.ErrResponseBodyTooLarge, requestContext) {
		return err
	}
	classifiedErr := openai.NewUpstreamStreamReadError(readErr.Unwrap())
	code, message, ok := openai.OpenAIUpstreamStreamReadErrorDetails(classifiedErr)
	if !ok {
		return err
	}
	payload, _ := json.Marshal(gin.H{
		"error": gin.H{
			"type":    "upstream_error",
			"code":    code,
			"message": message,
		},
	})
	var responseHeaders http.Header
	if resp != nil {
		responseHeaders = resp.Header
	}
	failoverErr := p.NewStreamPolicyFailure(
		c, provider, false, requestID, responseHeaders, http.StatusBadGateway, payload, message, false,
	)
	// 错误码在重试耗尽后保持稳定，客户端与透传规则据此识别传输故障。
	failoverErr.ResponseBody = payload
	return failoverErr
}
