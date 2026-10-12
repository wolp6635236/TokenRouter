package provider

import (
	"github.com/TokenFlux/TokenRouter/internal/egress"
	"github.com/TokenFlux/TokenRouter/internal/infra/httpclient/tlsfingerprint"
)

// TLSProfiles 将 TLS 策略服务返回的配置转换为传输指纹。
type TLSProfiles struct {
	*egress.TLSFingerprintProfileService
}

func NewTLSProfiles(core *egress.TLSFingerprintProfileService) *TLSProfiles {
	return &TLSProfiles{TLSFingerprintProfileService: core}
}

func (p *TLSProfiles) core() *egress.TLSFingerprintProfileService {
	if p == nil {
		return nil
	}
	return p.TLSFingerprintProfileService
}

// ResolveRequestTLS 根据提供商资格和路由匹配结果解析 TLS 策略，再转换为传输指纹。
func (p *TLSProfiles) ResolveRequestTLS(selection egress.TLSSelection) *tlsfingerprint.Profile {
	return ToTLSProfile(p.core().ResolveRequestPolicy(selection).TLSProfile)
}

func (p *TLSProfiles) ResolveTokenTLSProfileByID(id int64) (*tlsfingerprint.Profile, bool) {
	profile, ok := p.core().ResolveTokenTLSProfileByID(id)
	return ToTLSProfile(profile), ok
}

// ToTLSProfile 将 egress 的 TLS 模板转换为传输使用的指纹配置。
// 拨号器对空切片字段使用内置默认值。
func ToTLSProfile(p *egress.TLSFingerprintProfile) *tlsfingerprint.Profile {
	if p == nil {
		return nil
	}
	p = egress.CloneTLSFingerprintProfile(p)
	return &tlsfingerprint.Profile{
		Name:                p.Name,
		EnableGREASE:        p.EnableGREASE,
		CipherSuites:        p.CipherSuites,
		Curves:              p.Curves,
		PointFormats:        p.PointFormats,
		SignatureAlgorithms: p.SignatureAlgorithms,
		ALPNProtocols:       p.ALPNProtocols,
		SupportedVersions:   p.SupportedVersions,
		KeyShareGroups:      p.KeyShareGroups,
		PSKModes:            p.PSKModes,
		Extensions:          p.Extensions,
	}
}

// FromTLSProfile 复制传输指纹的各字段，生成出站策略使用的 TLS 模板。
func FromTLSProfile(p *tlsfingerprint.Profile) *egress.TLSFingerprintProfile {
	if p == nil {
		return nil
	}
	return egress.CloneTLSFingerprintProfile(&egress.TLSFingerprintProfile{Name: p.Name, EnableGREASE: p.EnableGREASE, CipherSuites: p.CipherSuites, Curves: p.Curves, PointFormats: p.PointFormats, SignatureAlgorithms: p.SignatureAlgorithms, ALPNProtocols: p.ALPNProtocols, SupportedVersions: p.SupportedVersions, KeyShareGroups: p.KeyShareGroups, PSKModes: p.PSKModes, Extensions: p.Extensions})
}
