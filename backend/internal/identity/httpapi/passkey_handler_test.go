package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/identity"
)

// TestPasskeyBeginLoginRequiresTencentCaptchaBeforeCeremony 检查缺少腾讯验证码票据时拒绝创建 Passkey 会话。
func TestPasskeyBeginLoginRequiresTencentCaptchaBeforeCeremony(t *testing.T) {
	authHandler, verifier := newOAuthCaptchaTestHandler(true)
	passkeys := identity.NewPasskeyService(false, nil, nil, nil, nil)
	handler := NewPasskeyHandler(passkeys, authHandler.Session.authService, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/passkey/login/begin", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.BeginLogin(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "TENCENT_CAPTCHA_VERIFICATION_FAILED")
	require.Zero(t, verifier.calls)
}

// TestPasskeyBeginLoginVerifiesTencentCaptchaBeforePasskeyService 检查票据通过后调用 Passkey 服务。
// 测试中的禁用实例返回功能关闭错误。
func TestPasskeyBeginLoginVerifiesTencentCaptchaBeforePasskeyService(t *testing.T) {
	authHandler, verifier := newOAuthCaptchaTestHandler(true)
	passkeys := identity.NewPasskeyService(false, nil, nil, nil, nil)
	handler := NewPasskeyHandler(passkeys, authHandler.Session.authService, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/passkey/login/begin",
		strings.NewReader(`{"tencent_captcha_ticket":"ticket-value","tencent_captcha_randstr":"@rand-value"}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.BeginLogin(c)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "PASSKEY_DISABLED")
	require.Equal(t, 1, verifier.calls)
	require.Equal(t, identity.TencentCaptchaProof{Ticket: "ticket-value", Randstr: "@rand-value"}, verifier.proof)
}
