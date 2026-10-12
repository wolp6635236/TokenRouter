package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
)

// Locale 将浏览器或 API 指定的语言传入业务上下文。
func Locale(defaultLanguage func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		fallback := locale.Default()
		if defaultLanguage != nil {
			fallback = defaultLanguage()
		}
		requested := c.GetHeader("Accept-Language")
		// 浏览器导航无法携带应用设置的 Header，首屏使用已保存的 Cookie。
		if !strings.HasPrefix(c.Request.URL.Path, "/api/") && strings.Contains(c.GetHeader("Accept"), "text/html") {
			if cookie, err := c.Cookie("tokenrouter_locale"); err == nil && locale.Normalize(cookie) != "" {
				requested = cookie
			}
		}
		code := locale.Negotiate(requested, fallback)
		ctx := locale.WithLanguage(c.Request.Context(), code)
		userFacing := !strings.HasPrefix(c.Request.URL.Path, "/api/v1/admin/") && !strings.HasPrefix(c.Request.URL.Path, "/admin/") && !strings.HasPrefix(c.Request.URL.Path, "/setup")
		c.Request = c.Request.WithContext(locale.WithUserPresentation(ctx, userFacing))
		c.Header("Content-Language", code)
		c.Writer.Header().Add("Vary", "Accept-Language")
		c.Next()
	}
}
