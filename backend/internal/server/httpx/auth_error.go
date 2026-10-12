package httpx

import (
	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
)

// ErrorResponse 标准错误响应结构。
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewErrorResponse 创建错误响应。
func NewErrorResponse(code, message string) ErrorResponse {
	return ErrorResponse{
		Code:    code,
		Message: message,
	}
}

// AbortWithError 中断请求并返回 JSON 错误。
func AbortWithError(c *gin.Context, statusCode int, code, message string) {
	language := locale.Default()
	if c.Request != nil {
		var selected bool
		language, selected = locale.Explicit(c.Request.Context())
		if !selected {
			language = locale.Negotiate(c.GetHeader("Accept-Language"), locale.Default())
		}
	}
	c.JSON(statusCode, NewErrorResponse(code, locale.ErrorText(language, code, statusCode, message)))
	c.Abort()
}
