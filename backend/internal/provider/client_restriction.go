package provider

const (
	// CodexClientRestrictionReasonDisabled 表示提供商未开启客户端访问限制。
	CodexClientRestrictionReasonDisabled = "openai_oauth_client_policy_any"
	// CodexClientRestrictionReasonMatchedUA 表示请求命中官方客户端 UA 白名单。
	CodexClientRestrictionReasonMatchedUA = "official_client_user_agent_matched"
	// CodexClientRestrictionReasonMatchedOriginator 表示请求命中官方客户端 originator 白名单。
	CodexClientRestrictionReasonMatchedOriginator = "official_client_originator_matched"
	// CodexClientRestrictionReasonMatchedAllowedClient 表示请求命中提供商级额外放行的命名客户端预设。
	CodexClientRestrictionReasonMatchedAllowedClient = "allowed_client_matched"
	// CodexClientRestrictionReasonMatchedGlobalAllowedClient 表示请求命中全局额外放行的命名客户端预设。
	CodexClientRestrictionReasonMatchedGlobalAllowedClient = "global_allowed_client_matched"
	// CodexClientRestrictionReasonNotMatchedUA 表示请求未命中官方客户端 UA 白名单。
	CodexClientRestrictionReasonNotMatchedUA = "official_client_user_agent_not_matched"
	// CodexClientRestrictionReasonForceCodexCLI 表示通过 ForceCodexCLI 配置放行。
	CodexClientRestrictionReasonForceCodexCLI = "force_codex_cli_enabled"
	// CodexClientRestrictionReasonMatchedTLSRouter 表示请求命中提供商绑定的 TLS 路由器。
	CodexClientRestrictionReasonMatchedTLSRouter = "tls_router_matched"
	// CodexClientRestrictionReasonNotMatchedTLSRouter 表示请求未命中提供商绑定的 TLS 路由器。
	CodexClientRestrictionReasonNotMatchedTLSRouter = "tls_router_not_matched"
	// CodexClientRestrictionReasonTLSRouterMissing 表示提供商策略要求 TLS 路由器命中，但提供商未绑定路由器。
	CodexClientRestrictionReasonTLSRouterMissing = "tls_router_missing"
)

// CodexClientRestrictionDetectionResult 是 codex_cli_only 统一检测入口结果。
type CodexClientRestrictionDetectionResult struct {
	Enabled bool
	Matched bool
	Reason  string
	Policy  string
}

// CodexClientOptions 注入平台字符串识别，核心只决定提供商客户端访问策略。
type CodexClientOptions struct {
	ForceCLI           bool
	OfficialUserAgent  func(string) bool
	OfficialOriginator func(string) bool
	AllowedClients     func(string, string, []string) bool
}

// ClientRestrictionDetector 只接收已读取的提供商与传输匹配结果，HTTP 元数据仍按需读取。
type ClientRestrictionDetector interface {
	DetectClient(func() (string, string), *Record, []string, bool) CodexClientRestrictionDetectionResult
}

// CodexClientDetector 绑定平台解析选项，所有策略仍由 DetectCodexClient 唯一执行。
type CodexClientDetector struct{ Options CodexClientOptions }

// DetectCodexClient 只按需读取客户端元数据，后台探针不需要构造 Gin 请求。
func DetectCodexClient(options CodexClientOptions, readClient func() (string, string), provider *Record, globalAllowedClients []string, routerMatched bool) CodexClientRestrictionDetectionResult {
	policy := OpenAIOAuthClientPolicyAny
	if provider != nil {
		policy = provider.GetOpenAIOAuthClientPolicy()
	}
	if provider == nil || policy == OpenAIOAuthClientPolicyAny {
		return CodexClientRestrictionDetectionResult{
			Enabled: false,
			Matched: false,
			Reason:  CodexClientRestrictionReasonDisabled,
			Policy:  policy,
		}
	}

	if options.ForceCLI {
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonForceCodexCLI,
			Policy:  policy,
		}
	}

	if policy == OpenAIOAuthClientPolicyTLSRouterMatchedOnly {
		if provider.GetTLSFingerprintRouterID() <= 0 {
			return CodexClientRestrictionDetectionResult{
				Enabled: true,
				Matched: false,
				Reason:  CodexClientRestrictionReasonTLSRouterMissing,
				Policy:  policy,
			}
		}
		if routerMatched {
			return CodexClientRestrictionDetectionResult{
				Enabled: true,
				Matched: true,
				Reason:  CodexClientRestrictionReasonMatchedTLSRouter,
				Policy:  policy,
			}
		}
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: false,
			Reason:  CodexClientRestrictionReasonNotMatchedTLSRouter,
			Policy:  policy,
		}
	}

	userAgent, originator := readClient()
	if options.OfficialUserAgent(userAgent) {
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonMatchedUA,
			Policy:  policy,
		}
	}
	if options.OfficialOriginator(originator) {
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonMatchedOriginator,
			Policy:  policy,
		}
	}

	// 官方客户端白名单未命中时，先尝试提供商级额外放行的命名客户端预设（如 Claude Code codex 插件）。
	if allowed := provider.GetCodexCLIOnlyAllowedClients(); len(allowed) > 0 &&
		options.AllowedClients(userAgent, originator, allowed) {
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonMatchedAllowedClient,
			Policy:  policy,
		}
	}

	// 再尝试由更高作用域（全局设置）注入的额外放行客户端列表。
	if len(globalAllowedClients) > 0 &&
		options.AllowedClients(userAgent, originator, globalAllowedClients) {
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonMatchedGlobalAllowedClient,
			Policy:  policy,
		}
	}

	return CodexClientRestrictionDetectionResult{
		Enabled: true,
		Matched: false,
		Reason:  CodexClientRestrictionReasonNotMatchedUA,
		Policy:  policy,
	}
}

func (d *CodexClientDetector) DetectClient(read func() (string, string), value *Record, allowed []string, matched bool) CodexClientRestrictionDetectionResult {
	return DetectCodexClient(d.Options, read, value, allowed, matched)
}
