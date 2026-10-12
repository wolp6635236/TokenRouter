package grok

import (
	"net/http"
	"os"
	"strings"

	"golang.org/x/mod/semver"
)

// 以下常量定义 Grok Build 与 CLI chat proxy 的固定客户端身份。
// 这些值刻意固化在二进制中，不从在线 CLI 抓取；运维可通过 XAI_GROK_CLI_VERSION 覆盖版本。
const (
	// CLIProxyHost 是必须附带官方 CLI 身份请求头的主机名。
	CLIProxyHost = "cli-chat-proxy.grok.com"

	// CLIStableVersion 是 cli-chat-proxy 已知可用的最低客户端版本。
	CLIStableVersion = "0.2.93"

	// CLIVersionEnv 是运维可选的 CLI 版本覆盖环境变量。
	CLIVersionEnv = "XAI_GROK_CLI_VERSION"

	// CLITokenAuth 是 cli-chat-proxy 接受 Grok Build OAuth token 时要求的认证类型。
	CLITokenAuth = "xai-grok-cli"

	// CLIClientIdentifier 是 Grok shell/CLI 使用的 x-grok-client-identifier 值。
	CLIClientIdentifier = "grok-shell"
)

// ResolveCLIVersion 返回受支持的 CLI 客户端版本。
// 空值或非法覆盖会回退到 billing.go 固定的首选 CLIClientVersion；
// CLIStableVersion 仅是最低允许版本，不是默认对外声明版本。
func ResolveCLIVersion() string {
	version := strings.TrimSpace(os.Getenv(CLIVersionEnv))
	if !IsSupportedCLIVersion(version) {
		return CLIClientVersion
	}
	return version
}

// IsSupportedCLIVersion 判断版本是否为规范 SemVer 且不低于 CLIStableVersion。
func IsSupportedCLIVersion(version string) bool {
	canonical := "v" + version
	minimum := "v" + CLIStableVersion
	return semver.IsValid(canonical) &&
		semver.Canonical(canonical) == canonical &&
		semver.Compare(canonical, minimum) >= 0
}

// CLIUserAgent 为指定 CLI 版本构造 workspace 风格 User-Agent。
func CLIUserAgent(version string) string {
	if strings.TrimSpace(version) == "" {
		version = CLIClientVersion
	}
	return "xai-grok-workspace/" + version
}

// ApplyCLIProxyHeaders 仅在目标为 cli-chat-proxy 时写入固定 Grok CLI 身份请求头，
// 直连 api.x.ai 的流量保持不变。
func ApplyCLIProxyHeaders(req *http.Request) {
	if req == nil || req.URL == nil || !strings.EqualFold(strings.TrimSpace(req.URL.Hostname()), CLIProxyHost) {
		return
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	version := ResolveCLIVersion()
	req.Header.Set("X-XAI-Token-Auth", CLITokenAuth)
	req.Header.Set("x-grok-client-version", version)
	req.Header.Set("x-grok-client-identifier", CLIClientIdentifier)
	req.Header.Set("User-Agent", CLIUserAgent(version))
}

// DefaultGrokUpstreamUserAgent 返回固定的 Grok CLI 或工作区用户代理。
// 请求头使用此身份覆盖入站客户端的 User-Agent。
func DefaultGrokUpstreamUserAgent() string {
	return CLIUserAgent(ResolveCLIVersion())
}

// ApplyDefaultGrokUpstreamHeaders 设置 Grok CLI 请求身份。
func ApplyDefaultGrokUpstreamHeaders(req *http.Request) {
	if req == nil {
		return
	}
	// xAI 对话与 CLI 接口识别客户端字符串，因此请求头使用 Grok CLI 身份。
	req.Header.Set("User-Agent", DefaultGrokUpstreamUserAgent())
	req.Header.Set("x-grok-client-version", ResolveCLIVersion())
	req.Header.Set("x-grok-client-identifier", CLIClientIdentifier)
}

// ApplyGrokRuntimeHeaders 设置运行时 Originator 和 Grok CLI 请求身份。
func ApplyGrokRuntimeHeaders(req *http.Request, runtimeOriginator string) {
	ApplyDefaultGrokUpstreamHeaders(req)
	if req == nil {
		return
	}
	// Originator 使用运行时配置，User-Agent 使用 Grok CLI 身份。
	if originator := strings.TrimSpace(runtimeOriginator); originator != "" {
		req.Header.Set("Originator", originator)
	}
	req.Header.Set("User-Agent", DefaultGrokUpstreamUserAgent())
}
