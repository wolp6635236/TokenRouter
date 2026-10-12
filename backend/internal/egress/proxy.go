package egress

import (
	"net"
	"net/url"
	"strconv"
	"time"
)

const (
	FallbackModeNone   = "none"
	FallbackModeProxy  = "proxy"
	FallbackModeDirect = "direct"

	// StatusActive 和 StatusExpired 是持久化的代理状态值。
	StatusActive  = "active"
	StatusExpired = "expired"
)

type Proxy struct {
	ID             int64
	Name           string
	Protocol       string
	Host           string
	Port           int
	Username       string
	Password       string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ExpiresAt      *time.Time
	FallbackMode   string
	BackupProxyID  *int64
	ExpiryWarnDays int
}

type ProxyWithProviderCount struct {
	Proxy
	ProviderCount  int64
	LatencyMs      *int64
	LatencyStatus  string
	LatencyMessage string
	IPAddress      string
	Country        string
	CountryCode    string
	Region         string
	City           string
	QualityStatus  string
	QualityScore   *int
	QualityGrade   string
	QualitySummary string
	QualityChecked *int64
}

type ProxyProviderSummary struct {
	ID       int64
	Name     string
	Platform string
	Type     string
	Notes    *string
}

// ProxyConnectionIdentity 包含连接代理所需的地址、凭据和状态。
type ProxyConnectionIdentity struct {
	Protocol string
	Host     string
	Port     int
	Username string
	Password string
	Status   string
}

func (p *Proxy) IsActive() bool {
	return p.Status == StatusActive
}

// IsExpired 报告代理是否已过期（基于 expires_at，与 status 无关）。
func (p *Proxy) IsExpired(now time.Time) bool {
	return p.ExpiresAt != nil && !p.ExpiresAt.After(now)
}

func (p *Proxy) URL() string {
	u := &url.URL{
		Scheme: p.Protocol,
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
	}
	if p.Username != "" && p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return u.String()
}

// ProxyConnectionIdentityFromProxy 提取代理的连接信息。
func ProxyConnectionIdentityFromProxy(proxyIn *Proxy) ProxyConnectionIdentity {
	return ProxyConnectionIdentity{
		Protocol: proxyIn.Protocol,
		Host:     proxyIn.Host,
		Port:     proxyIn.Port,
		Username: proxyIn.Username,
		Password: proxyIn.Password,
		Status:   proxyIn.Status,
	}
}
