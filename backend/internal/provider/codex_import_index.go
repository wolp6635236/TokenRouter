package provider

import (
	"strings"
)

type CodexProviderIndex struct {
	providersByKey   map[string][]Record
	keysByProviderID map[int64]map[string]struct{}
}

type CodexSeenIdentity struct {
	index  int
	userID string
}

// BuildCodexStoredIdentityKeys 生成存量提供商索引键，保留 user/provider 维度，
// 让 accessToken-only 提供商后续升级为完整 OAuth 时仍能命中并更新原提供商。
func BuildCodexStoredIdentityKeys(providerID, userID, email, accessToken string) []string {
	keys := make([]string, 0, 3)
	providerID = strings.TrimSpace(providerID)
	userID = strings.TrimSpace(userID)
	accessToken = strings.TrimSpace(accessToken)
	if userID != "" {
		keys = append(keys, "user:"+userID)
	}
	if providerID == "" && userID == "" {
		if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
			keys = append(keys, "email:"+email)
		}
	}
	if accessToken != "" {
		keys = append(keys, "access:"+CodexTokenFingerprint(accessToken))
	}
	if providerID != "" {
		keys = append(keys, "provider:"+providerID)
	}
	return keys
}

func BuildCodexProviderIndex(providers []Record) *CodexProviderIndex {
	index := &CodexProviderIndex{
		providersByKey:   map[string][]Record{},
		keysByProviderID: map[int64]map[string]struct{}{},
	}
	for _, provider := range providers {
		index.Add(provider)
	}
	return index
}

func (i *CodexProviderIndex) Add(provider Record) {
	if i == nil {
		return
	}
	// 索引不借用调用者的可变提供商图。
	provider = *CloneRecord(&provider)
	if i.providersByKey == nil {
		i.providersByKey = map[string][]Record{}
	}
	if i.keysByProviderID == nil {
		i.keysByProviderID = map[int64]map[string]struct{}{}
	}
	keys := BuildCodexStoredIdentityKeys(
		CodexCredentialString(provider.Credentials, "chatgpt_account_id"),
		CodexCredentialString(provider.Credentials, "chatgpt_user_id"),
		CodexCredentialString(provider.Credentials, "email"),
		CodexCredentialString(provider.Credentials, "access_token"),
	)
	orderedKeys := make([]string, 0, len(keys)+1)
	providerKeys := make(map[string]struct{}, len(keys)+1)
	for _, key := range keys {
		if _, exists := providerKeys[key]; exists {
			continue
		}
		providerKeys[key] = struct{}{}
		orderedKeys = append(orderedKeys, key)
	}
	if runtimeID := CodexCredentialString(provider.Credentials, "agent_runtime_id"); runtimeID != "" {
		key := "agent:" + runtimeID
		if _, exists := providerKeys[key]; !exists {
			providerKeys[key] = struct{}{}
			orderedKeys = append(orderedKeys, key)
		}
	}

	previousKeys := i.keysByProviderID[provider.ID]
	for key := range previousKeys {
		if _, retained := providerKeys[key]; retained {
			i.providersByKey[key] = upsertCodexProvider(i.providersByKey[key], provider)
			continue
		}
		i.removeFromKey(key, provider.ID)
	}
	for _, key := range orderedKeys {
		if _, retained := previousKeys[key]; retained {
			continue
		}
		i.providersByKey[key] = append(i.providersByKey[key], provider)
	}

	if len(providerKeys) > 0 {
		i.keysByProviderID[provider.ID] = providerKeys
		return
	}
	delete(i.keysByProviderID, provider.ID)
}

func (i *CodexProviderIndex) removeFromKey(key string, providerID int64) {
	providers := i.providersByKey[key]
	kept := providers[:0]
	for _, provider := range providers {
		if provider.ID != providerID {
			kept = append(kept, provider)
		}
	}
	if len(kept) == 0 {
		delete(i.providersByKey, key)
		return
	}
	i.providersByKey[key] = kept
}

// upsertCodexProvider 保留共享键的全部候选提供商，并原位替换已有提供商，
// 身份匹配有多个候选时，按候选顺序处理。
func upsertCodexProvider(providers []Record, provider Record) []Record {
	for idx := range providers {
		if providers[idx].ID == provider.ID {
			providers[idx] = provider
			return providers
		}
	}
	return append(providers, provider)
}

// Find 返回第一个通过跨用户校验的候选提供商及其命中的匹配键。
func (i *CodexProviderIndex) Find(keys []string, userID string) (*Record, string) {
	if i == nil {
		return nil, ""
	}
	for _, key := range keys {
		for _, provider := range i.providersByKey[key] {
			if codexIdentityConflicts(key, userID, CodexCredentialString(provider.Credentials, "chatgpt_user_id")) {
				continue
			}
			return CloneRecord(&provider), key
		}
	}
	return nil, ""
}

// codexIdentityConflicts 判断 provider: 键的命中是否把同一 ChatGPT 团队的两个
// 不同成员误连到一起：双方都携带 user id 且不相等时视为冲突。存量索引侧
// 仍保留 provider 键，任一侧缺少 user id 时允许匹配，使含 refresh_token
// 的常规导入和 accessToken-only 提供商升级为完整 OAuth 时仍能更新原提供商。
func codexIdentityConflicts(key, userID, storedUserID string) bool {
	if !strings.HasPrefix(key, "provider:") {
		return false
	}
	userID = strings.TrimSpace(userID)
	storedUserID = strings.TrimSpace(storedUserID)
	return userID != "" && storedUserID != "" && userID != storedUserID
}

func FirstSeenCodexIdentity(seen map[string]CodexSeenIdentity, keys []string, userID string) (int, bool) {
	for _, key := range keys {
		entry, ok := seen[key]
		if !ok {
			continue
		}
		if codexIdentityConflicts(key, userID, entry.userID) {
			continue
		}
		return entry.index, true
	}
	return 0, false
}

func MarkCodexIdentitySeen(seen map[string]CodexSeenIdentity, keys []string, index int, userID string) {
	for _, key := range keys {
		seen[key] = CodexSeenIdentity{index: index, userID: userID}
	}
}
