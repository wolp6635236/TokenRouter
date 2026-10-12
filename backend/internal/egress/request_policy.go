package egress

// EgressPolicy 包含本次请求使用的代理、TLS 配置、Header 和目标校验选项。
type EgressPolicy struct {
	ProxyURL           string                 `json:"-"`
	TLSProfile         *TLSFingerprintProfile `json:"-"`
	Headers            map[string]string      `json:"-"`
	TransportMode      string
	ValidateResolvedIP bool
	PublicHostsOnly    bool
	DisableRedirects   bool
}

// RequestPolicyInput 包含调用方已解析的出站参数。
// Header 在构造请求时应用，TLS 和重定向参数在获取客户端前确定。
type RequestPolicyInput struct {
	ProxyURL                                              string
	TLSProfile                                            *TLSFingerprintProfile
	Headers                                               map[string]string
	ValidateResolvedIP, PublicHostsOnly, DisableRedirects bool
}

// RequestPolicy 复制 TLS 配置，并按安全规则过滤 Header。
func RequestPolicy(input RequestPolicyInput) EgressPolicy {
	return EgressPolicy{ProxyURL: input.ProxyURL, TLSProfile: CloneTLSFingerprintProfile(input.TLSProfile), Headers: ResolveHeaderOverrides(input.Headers), ValidateResolvedIP: input.ValidateResolvedIP, PublicHostsOnly: input.PublicHostsOnly, DisableRedirects: input.DisableRedirects}
}

// RequiresHostValidation 在全局 IP 校验或请求级公网限制启用时返回 true。
func (p EgressPolicy) RequiresHostValidation() bool { return p.ValidateResolvedIP || p.PublicHostsOnly }

// String 返回包含传输模式的诊断文本。
func (p EgressPolicy) String() string { return "egress policy (" + p.TransportMode + ")" }

// GoString 返回 Go 语法格式化时使用的诊断文本。
func (p EgressPolicy) GoString() string { return p.String() }
