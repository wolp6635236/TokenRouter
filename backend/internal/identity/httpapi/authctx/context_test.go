package authctx

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/identity"
)

// TestPrincipalOwnsLegacyProjection 检查认证主体与 Gin 展示字段使用独立数据。
func TestPrincipalOwnsLegacyProjection(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	principal := identity.Principal{UserID: 7, Role: "user", SessionID: "session-1", CredentialKind: "jwt"}
	SetPrincipal(c, principal, 3, "test@example.invalid")
	c.Set(ContextKeyUser, AuthSubject{UserID: 99, Concurrency: 999})
	c.Set(ContextKeyUserRole, "admin")
	got, ok := GetPrincipal(c)
	require.True(t, ok)
	require.Equal(t, principal, got)
	subject, ok := GetAuthSubjectFromContext(c)
	require.True(t, ok)
	require.Equal(t, AuthSubject{UserID: 7, Concurrency: 3}, subject)
	role, ok := GetUserRoleFromContext(c)
	require.True(t, ok)
	require.Equal(t, "user", role)
}

// TestAPIKeyPrincipalKeepsActorSeparateFromPayer 检查 API Key 主体与付款用户的上下文分别存储。
func TestAPIKeyPrincipalKeepsActorSeparateFromPayer(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set(ContextKeyUser, AuthSubject{UserID: 11, Concurrency: 4})
	c.Set(ContextKeyUserRole, "admin")
	SetAuthenticatedPrincipal(c, identity.Principal{UserID: 22, CredentialKind: "api_key"})
	p, ok := GetPrincipal(c)
	require.True(t, ok)
	require.Equal(t, int64(22), p.UserID)
	require.Empty(t, p.Role)
	require.Empty(t, p.SessionID)
	subject, ok := GetAuthSubjectFromContext(c)
	require.True(t, ok)
	require.Equal(t, int64(11), subject.UserID)
	role, ok := GetUserRoleFromContext(c)
	require.True(t, ok)
	require.Equal(t, "admin", role, "旧网关展示投影仍按原付款用户提供")
}

// TestLegacyOnlyContextRemainsReadable 检查通过 Gin 字段读取用户身份和角色。
func TestLegacyOnlyContextRemainsReadable(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	c.Set(ContextKeyUser, AuthSubject{UserID: 9})
	c.Set(ContextKeyUserRole, "user")
	_, ok := GetPrincipal(c)
	require.False(t, ok)
	subject, ok := GetAuthSubjectFromContext(c)
	require.True(t, ok)
	require.Equal(t, int64(9), subject.UserID)
	role, ok := GetUserRoleFromContext(c)
	require.True(t, ok)
	require.Equal(t, "user", role)
}

func TestAuthSubjectHelpers_RoundTrip(t *testing.T) {
	c := &gin.Context{}
	c.Set(string(ContextKeyUser), AuthSubject{UserID: 1, Concurrency: 2})
	c.Set(string(ContextKeyUserRole), "admin")

	sub, ok := GetAuthSubjectFromContext(c)
	require.True(t, ok)
	require.Equal(t, int64(1), sub.UserID)
	require.Equal(t, 2, sub.Concurrency)

	role, ok := GetUserRoleFromContext(c)
	require.True(t, ok)
	require.Equal(t, "admin", role)
}
