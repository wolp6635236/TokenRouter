package authctx

import (
	"github.com/gin-gonic/gin"

	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry"
)

const (
	ContextKeyUser              = "user"
	ContextKeyUserRole          = "user_role"
	ContextKeyAuthEmail         = "auth_email"
	ContextKeySessionID         = "session_id"
	principalKey                = "identity_principal"
	MaxPersistentUserAgentBytes = 512
)

// AuthSubject 保存 Gin context 使用的用户 ID 和并发数，认证身份使用 Principal。
type AuthSubject struct {
	UserID      int64
	Concurrency int
}

// authenticationRecord 是请求认证的唯一来源，兼容字段供直接读取请求上下文的调用方使用。
// authenticationRecord 分别保存 API Key 的调用身份和付款用户资料，角色取自调用身份。
type authenticationRecord struct {
	Principal  identity.Principal
	Subject    AuthSubject
	HasSubject bool
	Role       string
	HasRole    bool
}

func SetPrincipal(c *gin.Context, p identity.Principal, concurrency int, email string) {
	if c.Request != nil {
		telemetry.UpdateRequest(c.Request.Context(), func(record *telemetry.RequestRecord) { record.UserID = p.UserID })
	}
	subject := AuthSubject{UserID: p.UserID, Concurrency: concurrency}
	c.Set(principalKey, authenticationRecord{Principal: p, Subject: subject, HasSubject: true, Role: p.Role, HasRole: true})
	c.Set(ContextKeyUser, subject)
	c.Set(ContextKeyUserRole, p.Role)
	c.Set(ContextKeyAuthEmail, email)
	c.Set(ContextKeySessionID, p.SessionID)
}

func recordFromContext(c *gin.Context) (authenticationRecord, bool) {
	v, ok := c.Get(principalKey)
	if !ok {
		return authenticationRecord{}, false
	}
	r, ok := v.(authenticationRecord)
	return r, ok
}

func GetPrincipal(c *gin.Context) (identity.Principal, bool) {
	r, ok := recordFromContext(c)
	return r.Principal, ok
}

func GetAuthSubjectFromContext(c *gin.Context) (AuthSubject, bool) {
	if r, ok := recordFromContext(c); ok {
		return r.Subject, r.HasSubject
	}
	v, ok := c.Get(ContextKeyUser)
	if !ok {
		return AuthSubject{}, false
	}
	s, ok := v.(AuthSubject)
	return s, ok
}

func GetUserRoleFromContext(c *gin.Context) (string, bool) {
	if r, ok := recordFromContext(c); ok {
		return r.Role, r.HasRole
	}
	v, ok := c.Get(ContextKeyUserRole)
	if !ok {
		return "", false
	}
	role, ok := v.(string)
	return role, ok
}

// SetAuthenticatedPrincipal 保存调用身份，并复制网关已解析的付款用户和并发资料。
func SetAuthenticatedPrincipal(c *gin.Context, p identity.Principal) {
	if c.Request != nil {
		telemetry.UpdateRequest(c.Request.Context(), func(record *telemetry.RequestRecord) { record.UserID = p.UserID })
	}
	record := authenticationRecord{Principal: p}
	if v, ok := c.Get(ContextKeyUser); ok {
		record.Subject, record.HasSubject = v.(AuthSubject)
	}
	if v, ok := c.Get(ContextKeyUserRole); ok {
		record.Role, record.HasRole = v.(string)
	}
	c.Set(principalKey, record)
}
