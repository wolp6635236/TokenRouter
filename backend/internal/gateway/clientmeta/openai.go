package clientmeta

import (
	"regexp"
	"strings"
)

const (
	// codexOfficialClientFamilyPrefix 覆盖 Codex Desktop 等 `Codex ` 家族标识。
	// 该值不能进入通用前缀列表，否则归一化会移除尾随空格并退化成裸 codex。
	codexOfficialClientFamilyPrefix = "codex "

	// codexOriginatorMaxLen 官方 clientInfo.name 均为短 ASCII 标识，远低于此上限。
	codexOriginatorMaxLen = 64

	// CodexDefaultOriginator 是网关默认使用的 Codex TUI originator。
	CodexDefaultOriginator = "codex-tui"
)

var (
	// CodexCLIUserAgentPrefixes 定义历史 Codex CLI User-Agent 前缀。
	// 示例："codex_vscode/1.0.0"、"codex_cli_rs/0.1.2"。
	CodexCLIUserAgentPrefixes = []string{
		"codex_vscode/",
		"codex_cli_rs/",
	}

	// CodexOfficialClientUserAgentPrefixes 定义 Codex 官方客户端家族 User-Agent 确定前缀。
	// `Codex ` 家族前缀需要保留尾随空格，单独由 codexOfficialClientFamilyPrefix 处理。
	CodexOfficialClientUserAgentPrefixes = []string{
		"codex_cli_rs/",
		"codex-tui/",
		"codex_vscode/",
		"codex_vscode_copilot/",
		"codex_app/",
		"codex_chatgpt_desktop/",
		"codex_atlas/",
		"codex_exec/",
		"codex_sdk_ts/",
	}

	// codexOfficialClientOriginators 列出 Codex 客户端 originator 的精确匹配值，codex_only 据此拒绝 evil-codex_cli 等相似名称。
	codexOfficialClientOriginators = map[string]bool{
		"codex_cli_rs":          true,
		"codex-tui":             true,
		"codex_vscode":          true,
		"codex_vscode_copilot":  true,
		"codex_app":             true,
		"codex_chatgpt_desktop": true,
		"codex_atlas":           true,
		"codex_exec":            true,
		"codex_sdk_ts":          true,
	}

	// codexEngineVersionPattern 提取版本段开头的三段数字 X.Y.Z（忽略 -alpha 等后缀）。
	codexEngineVersionPattern = regexp.MustCompile(`^(\d+\.\d+\.\d+)`)
)

// IsBrowserUserAgent 根据 Mozilla/ 前缀识别 Chrome、Firefox、Safari、Edge、Opera 等浏览器 UA。
// 该判断用于处理 OpenAI 上游接口对浏览器 UA 发出的 Cloudflare JavaScript 质询。
func IsBrowserUserAgent(userAgent string) bool {
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(ua), "mozilla/")
}

// IsCodexCLIRequest 判断 User-Agent 是否指向历史 Codex CLI 请求。
func IsCodexCLIRequest(userAgent string) bool {
	ua := NormalizeCodexClientHeader(userAgent)
	if ua == "" {
		return false
	}
	return matchCodexClientHeaderPrefixes(ua, CodexCLIUserAgentPrefixes)
}

// IsCodexOfficialClientRequest 检查 Codex 客户端 UA，兼容透传入口使用包含匹配作为后备条件。
func IsCodexOfficialClientRequest(userAgent string) bool {
	return isCodexOfficialClientRequest(userAgent, false)
}

// IsCodexOfficialClientRequestStrict 按官方 UA 前缀或可信尾部识别 Codex 请求，供 codex_only 访问检查使用。
func IsCodexOfficialClientRequestStrict(userAgent string) bool {
	return isCodexOfficialClientRequest(userAgent, true)
}

func isCodexOfficialClientRequest(userAgent string, strict bool) bool {
	ua := NormalizeCodexClientHeader(userAgent)
	if ua == "" {
		return false
	}
	if strict {
		if matchCodexClientHeaderStrictPrefixes(ua, CodexOfficialClientUserAgentPrefixes) {
			return true
		}
	} else if matchCodexClientHeaderPrefixes(ua, CodexOfficialClientUserAgentPrefixes) {
		return true
	}
	if strings.HasPrefix(ua, codexOfficialClientFamilyPrefix) {
		return true
	}
	if name := codexUATrailerName(ua); name != "" {
		return IsCodexOfficialClientOriginator(name)
	}
	return false
}

// codexUATrailerName 从 codex-rs UA 的最后一个括号组提取 clientInfo.name。
// CODEX_INTERNAL_ORIGINATOR_OVERRIDE 覆盖前缀，codex-rs engine 写入的 (name; version) 尾部仍保存客户端名称，
// 例如 cccc 前缀下可提取 codex-tui。输入需要先去除两侧空白，解析失败时返回空字符串。
// 函数按大小写无关方式解析，匹配调用方传入小写 UA，PairCodexClientIdentity 则传入原大小写以生成配套 originator。
func codexUATrailerName(ua string) string {
	_, after0, ok0 := strings.CutLast(ua, "(")
	if !ok0 {
		return ""
	}
	rest := after0
	before, _, ok := strings.Cut(rest, ")")
	if !ok {
		return ""
	}
	inner := strings.TrimSpace(before)
	if semi := strings.Index(inner, ";"); semi >= 0 {
		inner = strings.TrimSpace(inner[:semi])
	}
	return inner
}

// IsCodexOfficialClientOriginator 接受精确列表中的 originator 和 Codex 空格前缀的客户端家族。
func IsCodexOfficialClientOriginator(originator string) bool {
	v := NormalizeCodexClientHeader(originator)
	if v == "" {
		return false
	}
	if codexOfficialClientOriginators[v] {
		return true
	}
	return strings.HasPrefix(v, codexOfficialClientFamilyPrefix)
}

// IsCodexOfficialClientByHeaders 判断请求头是否指向 Codex 官方客户端家族。
func IsCodexOfficialClientByHeaders(userAgent, originator string) bool {
	return IsCodexOfficialClientRequest(userAgent) || IsCodexOfficialClientOriginator(originator)
}

func NormalizeCodexClientHeader(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func matchCodexClientHeaderPrefixes(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		normalizedPrefix := NormalizeCodexClientHeader(prefix)
		if normalizedPrefix == "" {
			continue
		}
		// 优先前缀匹配；若 UA/Originator 被网关拼接为复合字符串时，退化为包含匹配。
		if strings.HasPrefix(value, normalizedPrefix) || strings.Contains(value, normalizedPrefix) {
			return true
		}
	}
	return false
}

// matchCodexClientHeaderStrictPrefixes 按官方 UA 前缀匹配。
func matchCodexClientHeaderStrictPrefixes(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		normalizedPrefix := NormalizeCodexClientHeader(prefix)
		if normalizedPrefix == "" {
			continue
		}
		if strings.HasPrefix(value, normalizedPrefix) {
			return true
		}
	}
	return false
}

// PairCodexClientIdentity 从最终 User-Agent 推导配套 originator，必要时重写 UA 首段。
// /backend-api/codex 检查 originator 和 UA 首个斜杠前的名称，错配会返回 404（issue #3901，2026-07 实测）。
// 首段命中官方 originator 时直接配对并保留 UA；否则检查尾部 (name; version)，
// 尾部命中时用 name 重写首段，保留版本、OS 和终端信息。两处均未命中时返回 ok=false，调用方使用默认官方身份。
func PairCodexClientIdentity(userAgent string) (originator string, pairedUA string, ok bool) {
	ua := strings.TrimSpace(userAgent)
	slash := strings.IndexByte(ua, '/')
	if slash <= 0 {
		return "", "", false
	}
	if leading := strings.TrimSpace(ua[:slash]); isSaneCodexOriginator(leading) && IsCodexOfficialClientOriginator(leading) {
		leading = canonicalizeCodexOriginator(leading)
		return leading, leading + ua[slash:], true
	}
	// 按原大小写提取尾部，保留 Codex 家族的名称大小写。尾部含斜杠时拒绝，以保持 UA 首段和 originator 一致。
	if trailer := codexUATrailerName(ua); trailer != "" && !strings.ContainsRune(trailer, '/') &&
		isSaneCodexOriginator(trailer) && IsCodexOfficialClientOriginator(trailer) {
		trailer = canonicalizeCodexOriginator(trailer)
		return trailer, trailer + ua[slash:], true
	}
	return "", "", false
}

// isSaneCodexOriginator 检查 originator 长度及 ASCII 可打印字符，供 Codex 家族前缀匹配后的校验使用。
func isSaneCodexOriginator(name string) bool {
	if name == "" || len(name) > codexOriginatorMaxLen {
		return false
	}
	for i := range len(name) {
		if c := name[i]; c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

// canonicalizeCodexOriginator 把精确集合的官方 originator 大小写变体归一为规范小写形态
// （如 CODEX_CLI_RS → codex_cli_rs）；`Codex ` 家族不在精确集合中，保留原大小写
// （其规范形态本就是混合大小写，上游按大小写敏感 starts_with("Codex ") 判定）。
func canonicalizeCodexOriginator(name string) string {
	if lower := NormalizeCodexClientHeader(name); codexOfficialClientOriginators[lower] {
		return lower
	}
	return name
}

// ParseCodexEngineVersion 从 codex-rs 形态 UA 取引擎版本：
// `{originator}/{X.Y.Z} (...)`，第一个 '/' 后、首个空格或 '(' 前的三段版本。
// 该版本是 codex-rs CARGO_PKG_VERSION（引擎版本，CLI/app-server 一致）。
func ParseCodexEngineVersion(ua string) (string, bool) {
	ua = strings.TrimSpace(ua)
	slash := strings.IndexByte(ua, '/')
	if slash < 0 {
		return "", false
	}
	rest := ua[slash+1:]
	end := len(rest)
	for i := range len(rest) {
		if rest[i] == ' ' || rest[i] == '(' {
			end = i
			break
		}
	}
	m := codexEngineVersionPattern.FindString(strings.TrimSpace(rest[:end]))
	if m == "" {
		return "", false
	}
	return m, true
}
