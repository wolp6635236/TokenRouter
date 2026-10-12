package provider

import (
	"time"

	"github.com/google/uuid"

	acctcore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

// ConvergedInstallationID 返回提供商级恒定的 installation_id。
// 优先使用管理员配置的 device_id，缺省时从提供商随机种子派生。
func ConvergedInstallationID(value *acctcore.Record, seed string) string {
	if value == nil {
		return ""
	}
	if deviceID := value.GetOpenAIDeviceID(); deviceID != "" {
		return deviceID
	}
	if seed == "" {
		return ""
	}
	// 哈希使用兼容的种子值，升级后设备、会话及调用身份保持不变。
	return openai.DeriveStableUUIDv4("sub2api:codex-install-id:v2:" + seed)
}

func CodexFingerprintIDs(value *acctcore.Record, clientSessionID string, mode acctcore.CodexFingerprintMode) *openai.FingerprintIDs {
	if value == nil || mode == acctcore.CodexFingerprintOff {
		return nil
	}
	seed, ok := acctcore.CodexFingerprintSeed(value.Extra)
	if !ok {
		return nil
	}
	return openai.ResolveFingerprintIDs(value.ID, seed, clientSessionID, string(mode), func(seed string) string { return ConvergedInstallationID(value, seed) }, time.Now, func() string { return uuid.Must(uuid.NewV7()).String() })
}
