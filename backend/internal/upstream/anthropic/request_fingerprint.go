package anthropic

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	logger "github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
)

const (
	ClaudeCLIUserAgentProduct = "claude-cli"
	// 限制持久化 User-Agent 长度，避免异常客户端把超长值写入提供商缓存。
	MaxFingerprintUserAgentLength = 256
	// MaxClaudeCLIMajorVersionSkew 是 claude-cli 主版本号相对 tokenrouter 自身伪装
	// 版本（CLIVersion()）允许的最大超前量。给足两个大版本的升级
	// 窗口，同时挡掉 999 这类哨兵版本号。
	MaxClaudeCLIMajorVersionSkew = 2
)

var (
	// 预编译正则表达式（避免每次调用重新编译）
	// 匹配 User-Agent 版本号: xxx/x.y.z。
	UserAgentVersionRegex = regexp.MustCompile(`/(\d+)\.(\d+)\.(\d+)`)

	// 校验可写入提供商级持久指纹的 User-Agent 形态，版本号后只允许空白或结束。
	FingerprintUserAgentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/\d+\.\d+\.\d+(\s|$)`)

	// 默认指纹值（当客户端未提供时使用）。
	DefaultFingerprint = Fingerprint{
		UserAgent:               "claude-cli/" + CLIVersion() + " (external, cli)",
		StainlessLang:           "js",
		StainlessPackageVersion: "0.94.0",
		StainlessOS:             "Linux",
		StainlessArch:           "arm64",
		StainlessRuntime:        "node",
		StainlessRuntimeVersion: "v24.3.0",
	}
)

// Fingerprint represents provider fingerprint data.
type Fingerprint struct {
	ClientID                string
	UserAgent               string
	StainlessLang           string
	StainlessPackageVersion string
	StainlessOS             string
	StainlessArch           string
	StainlessRuntime        string
	StainlessRuntimeVersion string
	UpdatedAt               int64 `json:",omitempty"` // Unix timestamp，用于判断是否需要续期TTL
}

// FingerprintCache defines cache operations for identity service.
type FingerprintCache interface {
	GetFingerprint(ctx context.Context, providerID int64) (*Fingerprint, error)
	SetFingerprint(ctx context.Context, providerID int64, fp *Fingerprint) error
	// GetMaskedSessionID 获取固定的会话 ID（用于会话 ID 伪装功能）
	// 返回的 sessionID 是一个 UUID 格式的字符串
	// 如果不存在或已过期（15 分钟无请求），返回空字符串
	GetMaskedSessionID(ctx context.Context, providerID int64) (string, error)
	// SetMaskedSessionID 设置固定的会话 ID，TTL 为 15 分钟
	// 每次调用都会刷新 TTL
	SetMaskedSessionID(ctx context.Context, providerID int64, sessionID string) error
}

// RequestFingerprint 管理 OAuth 提供商的请求身份指纹。
type RequestFingerprint struct {
	cache FingerprintCache
}

// IsAcceptableFingerprintUserAgent 判断 User-Agent 是否适合作为提供商级持久身份。
// 指纹只升不降且会随活跃请求续期，因此本地构建后缀或不可信高版本一旦写入，
// 会长期覆盖同提供商的所有后续上游请求。
func IsAcceptableFingerprintUserAgent(userAgent string) bool {
	userAgent = strings.TrimSpace(userAgent)
	if userAgent == "" || len(userAgent) > MaxFingerprintUserAgentLength {
		return false
	}
	if !FingerprintUserAgentPattern.MatchString(userAgent) {
		return false
	}
	// 其它产品只校验稳定形态，避免把 Claude CLI 的版本窗口误用于第三方客户端。
	if ExtractProduct(userAgent) != ClaudeCLIUserAgentProduct {
		return true
	}
	major, _, _, ok := ParseUserAgentVersion(userAgent)
	if !ok {
		return false
	}
	currentMajor, _, _, currentOK := ParseUserAgentVersion(ClaudeCLIUserAgentProduct + "/" + CLIVersion())
	if !currentOK {
		return true
	}
	return major <= currentMajor+MaxClaudeCLIMajorVersionSkew
}

// NewRequestFingerprint 创建新的 RequestFingerprint。
func NewRequestFingerprint(cache FingerprintCache) *RequestFingerprint {
	return &RequestFingerprint{cache: cache}
}

// GetOrCreateFingerprint 获取或创建提供商的指纹
// 如果缓存存在，检测 user-agent 版本，新版本则更新
// 如果缓存不存在，生成随机 ClientID 并从请求头创建指纹，然后缓存。
func (s *RequestFingerprint) GetOrCreateFingerprint(ctx context.Context, providerID int64, headers http.Header) (*Fingerprint, error) {
	// 创建、升级和历史缓存自愈必须共用同一份校验结果。
	clientUA := strings.TrimSpace(headers.Get("User-Agent"))
	uaAcceptable := IsAcceptableFingerprintUserAgent(clientUA)

	// 尝试从缓存获取指纹
	cached, err := s.cache.GetFingerprint(ctx, providerID)
	if err == nil && cached != nil {
		needWrite := false

		// 仅在非法值原本会升级缓存时记录拒绝，避免异常客户端高频重试刷屏。
		if !uaAcceptable && clientUA != "" && IsNewerVersion(clientUA, cached.UserAgent) {
			logger.LegacyPrintf("service.identity",
				"Rejected fingerprint user-agent for provider %d: %q (malformed or implausible version)",
				providerID, clientUA)
		}

		if !IsAcceptableFingerprintUserAgent(cached.UserAgent) {
			// 历史污染指纹会被活跃提供商持续续期，读取时用合法客户端值或默认值自愈。
			poisonedUA := cached.UserAgent
			if uaAcceptable {
				MergeHeadersIntoFingerprint(cached, headers)
			} else {
				cached.UserAgent = DefaultFingerprint.UserAgent
			}
			needWrite = true
			logger.LegacyPrintf("service.identity",
				"Replaced malformed cached fingerprint for provider %d: %q -> %q",
				providerID, poisonedUA, cached.UserAgent)
		} else if uaAcceptable && IsNewerVersion(clientUA, cached.UserAgent) {
			// 版本升级时更新请求携带的字段，其余字段使用缓存值
			// 避免缺失的头被硬编码默认值覆盖（如新 CLI 版本 + 旧 SDK 默认值的不一致）
			MergeHeadersIntoFingerprint(cached, headers)
			needWrite = true
			logger.LegacyPrintf("service.identity", "Updated fingerprint for provider %d: %s (merge update)", providerID, clientUA)
		}

		if !needWrite && time.Since(time.Unix(cached.UpdatedAt, 0)) > 24*time.Hour {
			// 距上次写入超过 24 小时，续期 TTL
			needWrite = true
		}

		if needWrite {
			cached.UpdatedAt = time.Now().Unix()
			if err := s.cache.SetFingerprint(ctx, providerID, cached); err != nil {
				logger.LegacyPrintf("service.identity", "Warning: failed to refresh fingerprint for provider %d: %v", providerID, err)
			}
		}
		return cached, nil
	}

	// 首次创建也是持久化入口，非法值必须回退默认指纹。
	if !uaAcceptable && clientUA != "" {
		logger.LegacyPrintf("service.identity",
			"Rejected fingerprint user-agent for provider %d: %q (malformed or implausible version)",
			providerID, clientUA)
	}
	fp := s.createFingerprintFromHeaders(headers)

	// 生成随机 ClientID
	fp.ClientID = GenerateClientID()
	fp.UpdatedAt = time.Now().Unix()

	// 保存到缓存（7 天 TTL，每 24 小时自动续期）
	if err := s.cache.SetFingerprint(ctx, providerID, fp); err != nil {
		logger.LegacyPrintf("service.identity", "Warning: failed to cache fingerprint for provider %d: %v", providerID, err)
	}

	logger.LegacyPrintf("service.identity", "Created new fingerprint for provider %d with client_id: %s", providerID, fp.ClientID)
	return fp, nil
}

// createFingerprintFromHeaders 从请求头创建指纹。
func (s *RequestFingerprint) createFingerprintFromHeaders(headers http.Header) *Fingerprint {
	fp := &Fingerprint{}

	// 只有稳定形态且版本合理的 User-Agent 才能成为持久提供商身份。
	if ua := strings.TrimSpace(headers.Get("User-Agent")); IsAcceptableFingerprintUserAgent(ua) {
		fp.UserAgent = ua
	} else {
		fp.UserAgent = DefaultFingerprint.UserAgent
	}

	// 获取 x-stainless-*头，如果没有则使用默认值
	fp.StainlessLang = GetHeaderOrDefault(headers, "X-Stainless-Lang", DefaultFingerprint.StainlessLang)
	fp.StainlessPackageVersion = GetHeaderOrDefault(headers, "X-Stainless-Package-Version", DefaultFingerprint.StainlessPackageVersion)
	fp.StainlessOS = GetHeaderOrDefault(headers, "X-Stainless-OS", DefaultFingerprint.StainlessOS)
	fp.StainlessArch = GetHeaderOrDefault(headers, "X-Stainless-Arch", DefaultFingerprint.StainlessArch)
	fp.StainlessRuntime = GetHeaderOrDefault(headers, "X-Stainless-Runtime", DefaultFingerprint.StainlessRuntime)
	fp.StainlessRuntimeVersion = GetHeaderOrDefault(headers, "X-Stainless-Runtime-Version", DefaultFingerprint.StainlessRuntimeVersion)

	return fp
}

// MergeHeadersIntoFingerprint 将请求头中实际存在的字段合并到现有指纹中（用于版本升级场景）
// 请求携带的字段覆盖缓存，缺失的字段使用缓存值。
// 与 createFingerprintFromHeaders 的区别：后者用于首次创建，缺失头回退到 DefaultFingerprint；
// 版本升级时，缺失字段使用缓存值。
func MergeHeadersIntoFingerprint(fp *Fingerprint, headers http.Header) {
	// User-Agent：版本升级的触发条件，一定存在
	if ua := headers.Get("User-Agent"); ua != "" {
		fp.UserAgent = ua
	}
	// X-Stainless-* 头：仅在请求中实际携带时才更新，否则保留缓存值
	MergeHeader(headers, "X-Stainless-Lang", &fp.StainlessLang)
	MergeHeader(headers, "X-Stainless-Package-Version", &fp.StainlessPackageVersion)
	MergeHeader(headers, "X-Stainless-OS", &fp.StainlessOS)
	MergeHeader(headers, "X-Stainless-Arch", &fp.StainlessArch)
	MergeHeader(headers, "X-Stainless-Runtime", &fp.StainlessRuntime)
	MergeHeader(headers, "X-Stainless-Runtime-Version", &fp.StainlessRuntimeVersion)
}

// MergeHeader 如果请求头中存在该字段则更新目标值，否则保留原值。
func MergeHeader(headers http.Header, key string, target *string) {
	if v := headers.Get(key); v != "" {
		*target = v
	}
}

// GetHeaderOrDefault 获取 header 值，如果不存在则返回默认值。
func GetHeaderOrDefault(headers http.Header, key, defaultValue string) string {
	if v := headers.Get(key); v != "" {
		return v
	}
	return defaultValue
}

// ApplyFingerprint 将指纹应用到请求头（覆盖原有的 x-stainless-*头）
// 使用 setHeaderRaw 保持请求头的大小写，例如 X-Stainless-OS。
func (s *RequestFingerprint) ApplyFingerprint(req *http.Request, fp *Fingerprint) {
	if fp == nil {
		return
	}

	// 设置 user-agent
	if fp.UserAgent != "" {
		SetHeaderRaw(req.Header, "User-Agent", fp.UserAgent)
	}

	// 设置 x-stainless-*头（保持与 DefaultHeaders 一致的大小写）
	if fp.StainlessLang != "" {
		SetHeaderRaw(req.Header, "X-Stainless-Lang", fp.StainlessLang)
	}
	if fp.StainlessPackageVersion != "" {
		SetHeaderRaw(req.Header, "X-Stainless-Package-Version", fp.StainlessPackageVersion)
	}
	if fp.StainlessOS != "" {
		SetHeaderRaw(req.Header, "X-Stainless-OS", fp.StainlessOS)
	}
	if fp.StainlessArch != "" {
		SetHeaderRaw(req.Header, "X-Stainless-Arch", fp.StainlessArch)
	}
	if fp.StainlessRuntime != "" {
		SetHeaderRaw(req.Header, "X-Stainless-Runtime", fp.StainlessRuntime)
	}
	if fp.StainlessRuntimeVersion != "" {
		SetHeaderRaw(req.Header, "X-Stainless-Runtime-Version", fp.StainlessRuntimeVersion)
	}
}

// RewriteUserID 重写 body 中的 metadata.user_id
// 支持旧拼接格式和新 JSON 格式的 user_id 解析，
// 根据 fingerprintUA 版本选择输出格式。
//
// 重要：此函数使用 json.RawMessage 保留其他字段的原始字节，
// 避免重新序列化导致 thinking 块等内容被修改。
func (s *RequestFingerprint) RewriteUserID(body []byte, providerID int64, providerUUID, cachedClientID, fingerprintUA string) ([]byte, error) {
	if len(body) == 0 || providerUUID == "" || cachedClientID == "" {
		return body, nil
	}

	metadata := gjson.GetBytes(body, "metadata")
	if !metadata.Exists() || metadata.Type == gjson.Null {
		return body, nil
	}
	if !strings.HasPrefix(strings.TrimSpace(metadata.Raw), "{") {
		return body, nil
	}

	userIDResult := metadata.Get("user_id")
	if !userIDResult.Exists() || userIDResult.Type != gjson.String {
		return body, nil
	}
	userID := userIDResult.String()
	if userID == "" {
		return body, nil
	}

	// 解析 user_id（兼容旧拼接格式和新 JSON 格式）
	parsed := ParseMetadataUserID(userID)
	if parsed == nil {
		return body, nil
	}

	sessionTail := parsed.SessionID // 原始session UUID

	// 生成新的 session hash: SHA256(providerID::sessionTail) -> UUID 格式
	seed := fmt.Sprintf("%d::%s", providerID, sessionTail)
	newSessionHash := GenerateUUIDFromSeed(seed)

	// 根据客户端版本选择输出格式
	version := ExtractCLIVersion(fingerprintUA)
	newUserID := FormatMetadataUserID(cachedClientID, providerUUID, newSessionHash, version)
	if newUserID == userID {
		return body, nil
	}

	newBody, err := sjson.SetBytes(body, "metadata.user_id", newUserID)
	if err != nil {
		return body, nil
	}
	return newBody, nil
}

// RewriteUserIDWithMasking 重写 body 中的 metadata.user_id，支持会话 ID 伪装
// 如果提供商启用了会话 ID 伪装（session_id_masking_enabled），
// 则在完成常规重写后，将 session 部分替换为固定的伪装 ID（15 分钟内保持不变）
//
// 重要：此函数使用 json.RawMessage 保留其他字段的原始字节，
// 避免重新序列化导致 thinking 块等内容被修改。
func (s *RequestFingerprint) RewriteUserIDWithMasking(ctx context.Context, body []byte, providerID int64, masking bool, providerUUID, cachedClientID, fingerprintUA string) ([]byte, error) {
	// 先执行常规的 RewriteUserID 逻辑
	newBody, err := s.RewriteUserID(body, providerID, providerUUID, cachedClientID, fingerprintUA)
	if err != nil {
		return newBody, err
	}

	// 检查是否启用会话 ID 伪装
	if !masking {
		return newBody, nil
	}

	metadata := gjson.GetBytes(newBody, "metadata")
	if !metadata.Exists() || metadata.Type == gjson.Null {
		return newBody, nil
	}
	if !strings.HasPrefix(strings.TrimSpace(metadata.Raw), "{") {
		return newBody, nil
	}

	userIDResult := metadata.Get("user_id")
	if !userIDResult.Exists() || userIDResult.Type != gjson.String {
		return newBody, nil
	}
	userID := userIDResult.String()
	if userID == "" {
		return newBody, nil
	}

	// 解析已重写的 user_id
	uidParsed := ParseMetadataUserID(userID)
	if uidParsed == nil {
		return newBody, nil
	}

	// 获取或生成固定的伪装 session ID
	maskedSessionID, err := s.cache.GetMaskedSessionID(ctx, providerID)
	if err != nil {
		logger.LegacyPrintf("service.identity", "Warning: failed to get masked session ID for provider %d: %v", providerID, err)
		return newBody, nil
	}

	if maskedSessionID == "" {
		// 首次或已过期，生成新的伪装 session ID
		maskedSessionID = GenerateRandomUUID()
		logger.LegacyPrintf("service.identity", "Generated new masked session ID for provider %d: %s", providerID, maskedSessionID)
	}

	// 刷新 TTL（每次请求都刷新，保持 15 分钟有效期）
	if err := s.cache.SetMaskedSessionID(ctx, providerID, maskedSessionID); err != nil {
		logger.LegacyPrintf("service.identity", "Warning: failed to set masked session ID for provider %d: %v", providerID, err)
	}

	// 用 FormatMetadataUserID 重建（保持与 RewriteUserID 相同的格式）
	version := ExtractCLIVersion(fingerprintUA)
	newUserID := FormatMetadataUserID(uidParsed.DeviceID, uidParsed.AccountUUID, maskedSessionID, version)

	slog.Debug("session_id_masking_applied",
		"provider_id", providerID,
		"before", userID,
		"after", newUserID,
	)

	if newUserID == userID {
		return newBody, nil
	}

	maskedBody, setErr := sjson.SetBytes(newBody, "metadata.user_id", newUserID)
	if setErr != nil {
		return newBody, nil
	}
	return maskedBody, nil
}

// GenerateRandomUUID 生成随机 UUID v4 格式字符串。
func GenerateRandomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// fallback: 使用时间戳生成
		h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		b = h[:16]
	}

	// 设置 UUID v4 版本和变体位
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// GenerateClientID 生成 64 位十六进制客户端 ID（32 字节随机数）。
func GenerateClientID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// 极罕见的情况，使用时间戳+固定值作为 fallback
		logger.LegacyPrintf("service.identity", "Warning: crypto/rand.Read failed: %v, using fallback", err)
		// 使用 SHA256(当前纳秒时间)作为 fallback
		h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
		return hex.EncodeToString(h[:])
	}
	return hex.EncodeToString(b)
}

// GenerateUUIDFromSeed 从种子生成确定性 UUID v4 格式字符串。
func GenerateUUIDFromSeed(seed string) string {
	hash := sha256.Sum256([]byte(seed))
	bytes := hash[:16]

	// 设置 UUID v4 版本和变体位
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

// ParseUserAgentVersion 解析 user-agent 版本号
// 例如：claude-cli/2.1.2 -> (2, 1, 2)。
func ParseUserAgentVersion(ua string) (major, minor, patch int, ok bool) {
	// 匹配 xxx/x.y.z 格式
	matches := UserAgentVersionRegex.FindStringSubmatch(ua)
	if len(matches) != 4 {
		return 0, 0, 0, false
	}
	var err error
	if major, err = strconv.Atoi(matches[1]); err != nil {
		return 0, 0, 0, false
	}
	if minor, err = strconv.Atoi(matches[2]); err != nil {
		return 0, 0, 0, false
	}
	if patch, err = strconv.Atoi(matches[3]); err != nil {
		return 0, 0, 0, false
	}
	return major, minor, patch, true
}

// ExtractProduct 提取 User-Agent 中 "/" 前的产品名
// 例如：claude-cli/2.1.22 (external, cli) -> "claude-cli"。
func ExtractProduct(ua string) string {
	if idx := strings.Index(ua, "/"); idx > 0 {
		return strings.ToLower(ua[:idx])
	}
	return ""
}

// IsNewerVersion 比较版本号，判断 newUA 是否比 cachedUA 更新
// 要求产品名一致（防止浏览器 UA 如 Mozilla/5.0 误判为更新版本）。
func IsNewerVersion(newUA, cachedUA string) bool {
	// 校验产品名一致性
	newProduct := ExtractProduct(newUA)
	cachedProduct := ExtractProduct(cachedUA)
	if newProduct == "" || cachedProduct == "" || newProduct != cachedProduct {
		return false
	}

	newMajor, newMinor, newPatch, newOk := ParseUserAgentVersion(newUA)
	cachedMajor, cachedMinor, cachedPatch, cachedOk := ParseUserAgentVersion(cachedUA)

	if !newOk || !cachedOk {
		return false
	}

	// 比较版本号
	if newMajor > cachedMajor {
		return true
	}
	if newMajor < cachedMajor {
		return false
	}

	if newMinor > cachedMinor {
		return true
	}
	if newMinor < cachedMinor {
		return false
	}

	return newPatch > cachedPatch
}
