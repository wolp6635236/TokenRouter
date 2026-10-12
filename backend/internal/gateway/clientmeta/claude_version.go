package clientmeta

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

var claudeUAVersionPattern = regexp.MustCompile(`(?i)^claude-cli/(\d+\.\d+\.\d+)`)

// IsSupportedClaudeCLIVersion 校验运维设置的版本覆盖值。
// 版本需要为三段纯数字（如 2.1.251），并且不低于内置基线 CLICurrentVersion。
// -local、-dev、+build 等后缀会被 fingerprintUserAgentPattern 拒绝，写入持久指纹后可能持续触发上游 429，系统没有指纹重置入口。
// 降低版本基线也会降低身份服务检查客户端主版本超前时使用的基准。
func IsSupportedClaudeCLIVersion(version, minimum string) bool {
	version = strings.TrimSpace(version)
	if version == "" {
		return false
	}
	// Canonical 相等检查排除 v1.2 等省略段版本，再分别检查预发布和构建元数据。
	canonical := "v" + version
	if !semver.IsValid(canonical) || semver.Canonical(canonical) != canonical {
		return false
	}
	if semver.Prerelease(canonical) != "" || semver.Build(canonical) != "" {
		return false
	}
	return semver.Compare(canonical, "v"+minimum) >= 0
}

// CompareVersions 比较两个 semver 版本号
// 返回: -1 (a < b), 0 (a == b), 1 (a > b)。
func CompareVersions(a, b string) int {
	aParts := parseSemver(a)
	bParts := parseSemver(b)
	for i := range 3 {
		if aParts[i] < bParts[i] {
			return -1
		}
		if aParts[i] > bParts[i] {
			return 1
		}
	}
	return 0
}

// parseSemver 解析 semver 版本号为 [major, minor, patch]。
func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	result := [3]int{0, 0, 0}
	for i := 0; i < len(parts) && i < 3; i++ {
		if parsed, err := strconv.Atoi(parts[i]); err == nil {
			result[i] = parsed
		}
	}
	return result
}

func ExtractClaudeCLIVersion(ua string) string {
	matches := claudeUAVersionPattern.FindStringSubmatch(ua)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// ClaudeVersionRejection 根据传入的版本范围生成拒绝消息。
func ClaudeVersionRejection(clientVersion, minVersion, maxVersion string) string {
	if clientVersion == "" {
		return "Unable to determine Claude Code version. Please update Claude Code: npm update -g @anthropic-ai/claude-code"
	}

	if minVersion != "" && CompareVersions(clientVersion, minVersion) < 0 {
		return fmt.Sprintf("Your Claude Code version (%s) is below the minimum required version (%s). Please update: npm update -g @anthropic-ai/claude-code",
			clientVersion, minVersion)
	}

	if maxVersion != "" && CompareVersions(clientVersion, maxVersion) > 0 {
		return fmt.Sprintf("Your Claude Code version (%s) exceeds the maximum allowed version (%s). "+
			"Please downgrade: npm install -g @anthropic-ai/claude-code@%s && "+
			"set CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 to prevent auto-upgrade",
			clientVersion, maxVersion, maxVersion)
	}

	return ""
}
