package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// RefreshFailureNotice 不含凭据；Identity 仅用于当前进程区分认证版本，不进入 JSON 或日志。
type RefreshFailureNotice struct {
	ProviderID     int64
	Platform, Type string
	Identity       string `json:"-"`
	Until          time.Time
	Reason         string
}

// Prepare 在条件写入前读取停止代次，返回的发布函数按代次检查当前阻断状态。
type RefreshFailureObserver interface {
	PrepareRefreshFailure(int64) func(RefreshFailureNotice)
}

func (n RefreshFailureNotice) String() string {
	return fmt.Sprintf("refresh failure notice (provider_id=%d)", n.ProviderID)
}
func (n RefreshFailureNotice) GoString() string { return n.String() }

// PrepareRefreshFailureNotice 在写入前固定认证身份和通知代次，发布时据此检查阻断状态。
func PrepareRefreshFailureNotice(observer RefreshFailureObserver, value *Record) func(time.Time, string) {
	if observer == nil || value == nil {
		return func(time.Time, string) {}
	}
	notice := FailureNotice(value)
	publish := observer.PrepareRefreshFailure(value.ID)
	return func(until time.Time, reason string) {
		notice.Until = until
		notice.Reason = reason
		publish(notice)
	}
}

// RefreshCredentialIdentity 与凭据 CAS 使用相同平台/type/凭据/代理维度，不包含被失败操作改变的健康字段。
func RefreshCredentialIdentity(value *Record) string {
	if value == nil {
		return ""
	}
	credentials := value.Credentials
	if credentials == nil {
		credentials = map[string]any{}
	}
	data, err := json.Marshal(struct {
		Platform, Type string
		Credentials    map[string]any
		ProxyID        *int64
	}{value.Platform, value.Type, credentials, value.ProxyID})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func FailureNotice(value *Record) RefreshFailureNotice {
	if value == nil {
		return RefreshFailureNotice{}
	}
	return RefreshFailureNotice{ProviderID: value.ID, Platform: value.Platform, Type: value.Type, Identity: RefreshCredentialIdentity(value)}
}
