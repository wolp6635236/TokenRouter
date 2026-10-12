package openai

import (
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/TokenFlux/TokenRouter/internal/gateway/clientmeta"
)

const (
	// CodexUpstreamMinVersion 上游 /backend-api/codex 接受的最低 version 头：
	// 若请求携带 version 且低于该值，上游直接 404（issue #3901，2026-07 实测）。
	CodexUpstreamMinVersion = "0.144.0"

	CodexClientVersionMaxLen = 64

	CodexCLIVersion   = "0.144.1"
	CodexCLIUserAgent = CodexDefaultOriginator + "/" + CodexCLIVersion + " (Ubuntu 22.4.0; x86_64) xterm-256color"

	CodexDefaultOriginator = clientmeta.CodexDefaultOriginator
)

var (
	codexClientVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,3}(-[0-9A-Za-z.]+)?$`)

	codexCanonicalUAMu       sync.RWMutex
	codexCanonicalUAResolver func() string
)

type CodexOutboundIdentity struct {
	UserAgent  string
	Originator string
	Version    string
}

// SetCodexCanonicalUserAgentResolver 注入后台设置提供的规范 Codex UA 解析器。
// 无法注入或解析失败时，所有无提供商出站路径回退到编译期默认身份。
func SetCodexCanonicalUserAgentResolver(resolver func() string) {
	codexCanonicalUAMu.Lock()
	defer codexCanonicalUAMu.Unlock()
	codexCanonicalUAResolver = resolver
}

// CodexCanonicalUserAgent 返回当前生效的规范 Codex User-Agent。
func CodexCanonicalUserAgent() string {
	return ResolveCodexOutboundIdentity("").UserAgent
}

// CodexCanonicalAuthIdentity 返回凭据面使用的身份对；凭据面不需要 version 头。
func CodexCanonicalAuthIdentity() (userAgent, originator string) {
	identity := ResolveCodexOutboundIdentity("")
	return identity.UserAgent, identity.Originator
}

// ApplyCodexCanonicalAuthIdentity 为凭据面请求写入规范 UA 与 originator。
func ApplyCodexCanonicalAuthIdentity(h http.Header) {
	if h == nil {
		return
	}
	userAgent, originator := CodexCanonicalAuthIdentity()
	h.Set("user-agent", userAgent)
	h.Set("originator", originator)
}

// CodexCanonicalClientVersion 返回与规范 UA 同源的版本号。
func CodexCanonicalClientVersion() string {
	return ResolveCodexOutboundIdentity("").Version
}

func ConfiguredCodexUserAgent() string {
	codexCanonicalUAMu.RLock()
	resolver := codexCanonicalUAResolver
	codexCanonicalUAMu.RUnlock()
	if resolver != nil {
		if value := strings.TrimSpace(resolver()); value != "" {
			return value
		}
	}
	return CodexCLIUserAgent
}

// NormalizeCodexClientVersion 只接受短 ASCII 版本号，避免异常值进入出站头。
func NormalizeCodexClientVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > CodexClientVersionMaxLen || !codexClientVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

func ResolveCodexOutboundIdentity(candidateUA string) CodexOutboundIdentity {
	canonical := ConfiguredCodexUserAgent()
	ua := strings.TrimSpace(candidateUA)
	if ua == "" {
		ua = canonical
	}
	originator, pairedUA, ok := clientmeta.PairCodexClientIdentity(ua)
	if !ok {
		originator, pairedUA, ok = clientmeta.PairCodexClientIdentity(canonical)
	}
	if !ok {
		originator, pairedUA = clientmeta.CodexDefaultOriginator, CodexCLIUserAgent
	}
	version := CodexClientVersionFromUA(canonical)
	if rebuilt := ReplaceCodexUserAgentVersion(pairedUA, version); rebuilt != "" {
		pairedUA = rebuilt
	}
	return CodexOutboundIdentity{UserAgent: pairedUA, Originator: originator, Version: version}
}

func CodexClientVersionFromUA(userAgent string) string {
	version, ok := clientmeta.ParseCodexEngineVersion(userAgent)
	if !ok {
		return CodexCLIVersion
	}
	version = NormalizeCodexClientVersion(version)
	if version == "" || clientmeta.CompareVersions(version, CodexUpstreamMinVersion) < 0 {
		return CodexCLIVersion
	}
	return version
}

func ReplaceCodexUserAgentVersion(userAgent, version string) string {
	version = NormalizeCodexClientVersion(version)
	if version == "" {
		return ""
	}
	slash := strings.IndexByte(userAgent, '/')
	if slash <= 0 || slash+1 >= len(userAgent) {
		return ""
	}
	end := slash + 1
	for end < len(userAgent) && userAgent[end] != ' ' && userAgent[end] != '(' {
		end++
	}
	return userAgent[:slash+1] + version + userAgent[end:]
}

// EnsureCodexIdentityHeaders 补齐 OAuth（ChatGPT 内部接口）出站请求所需的 Codex 身份头。
// 已有 User-Agent 与 version 保持不变，交给紧随其后的 EnforceCodexIdentityHeaders
// 做官方身份配对与最低版本校正。
func EnsureCodexIdentityHeaders(h http.Header) {
	if h == nil {
		return
	}
	identity := ResolveCodexOutboundIdentity("")
	if strings.TrimSpace(h.Get("user-agent")) == "" {
		h.Set("user-agent", identity.UserAgent)
	}
	if strings.TrimSpace(h.Get("originator")) == "" {
		h.Set("originator", identity.Originator)
	}
	if strings.TrimSpace(h.Get("version")) == "" {
		h.Set("version", identity.Version)
	}
	h.Set("OpenAI-Beta", "responses=experimental")
}

// ApplyOpenAICodexProbeHeaders 为合成 Responses 探测请求补齐 Codex 身份和窗口标识。
func ApplyOpenAICodexProbeHeaders(h http.Header) {
	if h == nil {
		return
	}
	EnsureCodexIdentityHeaders(h)
	h.Set("X-Codex-Window-ID", uuid.NewString())
}

// EnforceCodexIdentityHeaders 收口 OAuth（ChatGPT 内部接口）出站请求的客户端身份头。
// 上游要求 originator 与 User-Agent 首段配套且为官方客户端标识，version 头（若携带）
// 不低于 0.144.0，任一不满足即 404（issue #3901）。以最终 User-Agent 为准推导配套
// originator；推导不出官方身份（第三方 UA / UA 缺失）时整体回退为默认 Codex TUI 身份。
//
// 仅对携带 originator 的请求生效；需要从缺失身份头恢复的调用方应先调用
// EnsureCodexIdentityHeaders。
// 必须在所有 User-Agent 改写（自定义 UA / ForceCodexCLI / 浏览器 UA 兜底）之后调用。
func EnforceCodexIdentityHeaders(h http.Header) {
	EnforceCodexIdentityHeadersWithUA(h, "")
}

// EnforceCodexIdentityHeadersWithUA 保留官方 UA 的客户端名与设备指纹，
// 仅在无法配对或版本过旧时回退到 canonical 身份；overrideUA 供提供商级配置调用方使用。
func EnforceCodexIdentityHeadersWithUA(h http.Header, overrideUA string) {
	if h == nil || h.Get("originator") == "" {
		return
	}
	candidateUA := strings.TrimSpace(overrideUA)
	if candidateUA == "" {
		candidateUA = h.Get("user-agent")
	}
	originator, pairedUA, ok := clientmeta.PairCodexClientIdentity(candidateUA)
	if !ok {
		identity := ResolveCodexOutboundIdentity("")
		originator, pairedUA = identity.Originator, identity.UserAgent
	}
	h.Set("user-agent", pairedUA)
	h.Set("originator", originator)
	if v := strings.TrimSpace(h.Get("version")); v != "" && clientmeta.CompareVersions(v, CodexUpstreamMinVersion) < 0 {
		h.Set("version", CodexCanonicalClientVersion())
	}
}

// ResolveUpstreamOriginator 按路由覆盖、客户端原值、官方默认值和兼容默认值的顺序选择 originator。
func ResolveUpstreamOriginator(read func() string, official, matched bool, routed string) string {
	if matched {
		if value := strings.TrimSpace(routed); value != "" {
			return value
		}
	}
	if value := strings.TrimSpace(read()); value != "" {
		return value
	}
	if official {
		return ResolveCodexOutboundIdentity("").Originator
	}
	return "opencode"
}

// IsBrowserUserAgent 判断 User-Agent 是否属于浏览器。
func IsBrowserUserAgent(userAgent string) bool { return clientmeta.IsBrowserUserAgent(userAgent) }

// IsCodexOfficialClientRequest 判断请求是否来自官方 Codex 客户端。
func IsCodexOfficialClientRequest(userAgent string) bool {
	return clientmeta.IsCodexOfficialClientRequest(userAgent)
}

// IsCodexOfficialClientRequestStrict 按完整身份格式识别官方 Codex 客户端。
func IsCodexOfficialClientRequestStrict(userAgent string) bool {
	return clientmeta.IsCodexOfficialClientRequestStrict(userAgent)
}

// IsCodexOfficialClientOriginator 识别官方 Codex originator。
func IsCodexOfficialClientOriginator(originator string) bool {
	return clientmeta.IsCodexOfficialClientOriginator(originator)
}

// IsCodexOfficialClientByHeaders 根据 User-Agent 和 originator 识别官方客户端。
func IsCodexOfficialClientByHeaders(userAgent, originator string) bool {
	return clientmeta.IsCodexOfficialClientByHeaders(userAgent, originator)
}

// PairCodexClientIdentity 配对 User-Agent 与 originator。
func PairCodexClientIdentity(userAgent string) (originator string, pairedUA string, ok bool) {
	return clientmeta.PairCodexClientIdentity(userAgent)
}

// normalizeCodexClientHeader 归一化客户端许可策略使用的请求头。
func normalizeCodexClientHeader(value string) string {
	return clientmeta.NormalizeCodexClientHeader(value)
}
