package apikey

import (
	"context"
	"fmt"
)

const (
	AuthenticationLookup       AuthenticationFailureKind = "lookup"
	AuthenticationDisabled     AuthenticationFailureKind = "disabled"
	AuthenticationTeam         AuthenticationFailureKind = "team"
	AuthenticationMemberLimit  AuthenticationFailureKind = "member_limit"
	AuthenticationIP           AuthenticationFailureKind = "ip"
	AuthenticationUserMissing  AuthenticationFailureKind = "user_missing"
	AuthenticationUserInactive AuthenticationFailureKind = "user_inactive"
)

// @project-doc docs/architecture/gateway_request_lifecycle.md#apikey_authentication
// AccessSnapshot 明确区分凭据所有者、付款用户与行为成员；资金来源由 billing 另行解析。
// key 是本次认证的独立副本，L1/L2 保存各自的快照。
type AccessSnapshot struct {
	KeyID          int64
	OwnerUserID    int64
	PayerUserID    int64
	ActorUserID    int64
	TeamID         *int64
	key            *APIKey
	fastModePolicy string
}

// AuthenticationInput 提供 HTTP 层已经解析的地址与消费入口意图，不读取请求体。
type AuthenticationInput struct {
	ClientIP          string
	CheckMemberLimits bool
}

type AuthenticationFailureKind string

// AuthenticationFailure 只携带失败阶段，协议状态码和错误 envelope 由 HTTP 适配器决定。
type AuthenticationFailure struct {
	Kind     AuthenticationFailureKind
	Cause    error
	ClientIP string
}

// ipAccessDenied 保留既有公开错误文本；HTTP 适配独立决定响应形状。
type ipAccessDenied string

// KeyView 将认证快照转换为本次网关请求使用的 APIKey。
func (a *AccessSnapshot) KeyView() *APIKey {
	if a == nil {
		return nil
	}
	return a.key
}

func (e *AuthenticationFailure) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Kind)
}

func (e *AuthenticationFailure) Unwrap() error { return e.Cause }

// Authenticate 保留 Key/团队/成员限额/IP/付款用户的既有检查顺序。
// 认证失败时，返回的非空快照用于诊断。调用方根据错误判断认证结果。
// Key 过期与额度耗尽仍由之后的资金准入处理，非消费入口可以读取已有数据。
func (s *APIKeyService) Authenticate(ctx context.Context, credential string, input AuthenticationInput) (*AccessSnapshot, error) {
	key, err := s.GetByKey(ctx, credential)
	if err != nil {
		return nil, &AuthenticationFailure{Kind: AuthenticationLookup, Cause: err}
	}
	return s.authenticateKey(key, input)
}

// authenticateKey 统一校验已加载身份，供普通认证与长连接逐轮复核共用。
func (s *APIKeyService) authenticateKey(key *APIKey, input AuthenticationInput) (*AccessSnapshot, error) {
	if key == nil {
		return nil, &AuthenticationFailure{Kind: AuthenticationLookup, Cause: ErrAPIKeyNotFound}
	}
	access := &AccessSnapshot{KeyID: key.ID, OwnerUserID: key.UserID, ActorUserID: key.UserID, TeamID: clonePointer(key.TeamID), key: key, fastModePolicy: key.FastModePolicy}
	if key.User != nil {
		access.PayerUserID = key.User.ID
	}
	if key.ActorUser != nil {
		access.ActorUserID = key.ActorUser.ID
	}
	fail := func(kind AuthenticationFailureKind, err error) (*AccessSnapshot, error) {
		return access, &AuthenticationFailure{Kind: kind, Cause: err}
	}
	if !key.IsActive() && key.Status != StatusAPIKeyExpired && key.Status != StatusAPIKeyQuotaExhausted {
		return fail(AuthenticationDisabled, nil)
	}
	if err := s.ValidateTeamKeyLifecycle(key); err != nil {
		return fail(AuthenticationTeam, err)
	}
	if input.CheckMemberLimits {
		if err := s.CheckTeamMemberLimits(key); err != nil {
			return fail(AuthenticationMemberLimit, err)
		}
	}
	if len(key.IPWhitelist) > 0 || len(key.IPBlacklist) > 0 {
		if allowed, _ := CheckIPRestrictionWithCompiledRules(input.ClientIP, key.CompiledIPWhitelist, key.CompiledIPBlacklist); !allowed {
			ip := input.ClientIP
			if ip == "" {
				ip = "unknown"
			}
			return access, &AuthenticationFailure{Kind: AuthenticationIP, ClientIP: ip, Cause: ipAccessDenied(ip)}
		}
	}
	if key.User == nil {
		return fail(AuthenticationUserMissing, nil)
	}
	if !key.User.IsActive() {
		return fail(AuthenticationUserInactive, nil)
	}
	return access, nil
}

func (e ipAccessDenied) Error() string { return fmt.Sprintf("Access denied. Your IP is %s", string(e)) }
