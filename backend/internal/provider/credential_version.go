package provider

import (
	"context"
	"fmt"
)

// CredentialMutationSnapshot 是某次凭据使用所观察到的身份，供条件写入比较。
type CredentialMutationSnapshot struct {
	CredentialsJSON string `json:"-"`
	AccessToken     string `json:"-"`
	RefreshToken    string `json:"-"`
	TokenVersion    int64  `json:"-"`
	ProxyID         *int64 `json:"-"`
}

// CredentialVersion 是刷新条件写入时比较的身份数据。
// 提供商身份和代理与交换前的完整凭据一起比较，不能只比较 access_token。
type CredentialVersion struct {
	ID          int64
	Platform    string
	Type        string
	Status      string
	ProxyID     *int64
	Credentials map[string]any `json:"-"`
}

// CredentialRefreshWriter 仅在交换使用的状态仍然有效时保存新凭据。
// false 表示状态发生改变，调用方必须重新读取，不能再次交换或覆盖新值。
type CredentialRefreshWriter interface {
	UpdateOAuthCredentialsIfUnchanged(context.Context, CredentialVersion, map[string]any) (bool, error)
}

func (CredentialMutationSnapshot) String() string { return "CredentialMutationSnapshot{已脱敏}" }

func (s CredentialMutationSnapshot) GoString() string { return s.String() }

func (v CredentialVersion) String() string {
	return fmt.Sprintf("provider credential version (id=%d)", v.ID)
}

func (v CredentialVersion) GoString() string { return v.String() }

// CloneCredentialVersion 冻结等待锁期间的比较输入，不让调用方改变嵌套凭据。
func CloneCredentialVersion(value CredentialVersion) CredentialVersion {
	value.Credentials = CloneValues(value.Credentials)
	value.ProxyID = clonePointer(value.ProxyID)
	return value
}

// MatchesCredentialVersion 按数据库 CAS 的身份字段比较凭据，nil 凭据按空对象比较。
func MatchesCredentialVersion(value *Record, expected CredentialVersion) bool {
	if value == nil || value.ID != expected.ID || value.Status != expected.Status {
		return false
	}
	identity := RefreshCredentialIdentity(value)
	return identity != "" && identity == RefreshCredentialIdentity(&Record{Platform: expected.Platform, Type: expected.Type, Credentials: expected.Credentials, ProxyID: expected.ProxyID})
}
