package notification

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildVerifyCodeEmailBody_EscapesSiteName(t *testing.T) {
	t.Run("escapes_script_injection", func(t *testing.T) {
		body := renderIdentityBody(t, NotificationEmailEventAuthVerifyCode, "123456", `</h1><script>alert(1)</script><h1>`)

		assert.NotContains(t, body, "<script>")
		assert.Contains(t, body, "&lt;script&gt;")
	})

	t.Run("escapes_html_entities", func(t *testing.T) {
		body := renderIdentityBody(t, NotificationEmailEventAuthVerifyCode, "123456", `A&B<C>"D`)

		assert.Contains(t, body, "A&amp;B&lt;C&gt;&#34;D")
	})

	t.Run("keeps_normal_site_name", func(t *testing.T) {
		body := renderIdentityBody(t, NotificationEmailEventAuthVerifyCode, "654321", "My Site")

		assert.Contains(t, body, "My Site")
		assert.Contains(t, body, "654321")
	})
}

func TestBuildPasswordResetEmailBody_EscapesHTMLValues(t *testing.T) {
	t.Run("escapes_html_tags_in_site_name", func(t *testing.T) {
		body := renderIdentityBody(t, NotificationEmailEventAuthPasswordReset, "https://example.com/reset?token=abc", `</h1><img src=x onerror=alert(1)>`)

		assert.NotContains(t, body, "<img src=x")
		assert.Contains(t, body, "&lt;img")
	})

	t.Run("escapes_html_entities", func(t *testing.T) {
		body := renderIdentityBody(t, NotificationEmailEventAuthPasswordReset, "https://example.com/reset", `A&B<C>`)

		assert.Contains(t, body, "A&amp;B&lt;C&gt;")
	})

	t.Run("keeps_normal_site_name_and_url", func(t *testing.T) {
		resetURL := "https://example.com/reset?token=xyz"
		body := renderIdentityBody(t, NotificationEmailEventAuthPasswordReset, resetURL, "TokenRouter")

		assert.Contains(t, body, "TokenRouter")
		assert.Contains(t, body, resetURL)
	})

	t.Run("escapes_ampersand_in_reset_url", func(t *testing.T) {
		resetURL := "https://example.com/reset?a=1&b=2"
		body := renderIdentityBody(t, NotificationEmailEventAuthPasswordReset, resetURL, "Site")

		assert.NotContains(t, body, `href="https://example.com/reset?a=1&b=2"`)
		assert.Contains(t, body, `href="https://example.com/reset?a=1&amp;b=2"`)
	})
}

// renderIdentityBody 使用实际发送的内置模板检查变量转义。
func renderIdentityBody(t *testing.T, event, value, siteName string) string {
	t.Helper()
	template := notificationEmailOfficialTemplates[event][notificationEmailLocaleChinese]
	rendered, err := RenderNotificationEmail(event, template.Subject, template.HTML, map[string]string{"site_name": siteName, "verification_code": value, "reset_url": value, "expires_in_minutes": "15"}, nil)
	assert.NoError(t, err)
	return rendered.HTML
}
