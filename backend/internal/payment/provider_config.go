package payment

import "encoding/json"

// parseProviderConfig 读取 JSON 或 AES-256-GCM 密文，第二个返回值表示配置可读。
func parseProviderConfig(stored string, encryptionKey []byte) (map[string]string, bool) {
	if stored == "" {
		return nil, true
	}
	var config map[string]string
	if err := json.Unmarshal([]byte(stored), &config); err == nil {
		return config, true
	}
	// 历史支付实例可能仍存有密文，需要使用部署时配置的密钥读取。
	if len(encryptionKey) == AES256KeySize {
		//nolint:staticcheck // SA1019: 历史密文仍需要读取。
		if plaintext, err := Decrypt(stored, encryptionKey); err == nil {
			if err := json.Unmarshal([]byte(plaintext), &config); err == nil {
				return config, true
			}
		}
	}
	return nil, false
}
