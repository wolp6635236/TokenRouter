package session

import (
	"fmt"
	"sync"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocolgemini "github.com/TokenFlux/TokenRouter/internal/protocol/gemini"
)

func TestDigestSessionStore_SaveAndFind(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "s:a1-u:b2-m:c3", "uuid-1", 100, "")

	uuid, providerID, _, found := store.Find(1, "prefix", "s:a1-u:b2-m:c3")
	require.True(t, found)
	assert.Equal(t, "uuid-1", uuid)
	assert.Equal(t, int64(100), providerID)
}

func TestDigestSessionStore_PrefixMatch(t *testing.T) {
	store := NewDigestSessionStore()

	// 保存短链
	store.Save(1, "prefix", "u:a-m:b", "uuid-short", 10, "")

	// 用长链查找，应前缀匹配到短链
	uuid, providerID, matchedChain, found := store.Find(1, "prefix", "u:a-m:b-u:c-m:d")
	require.True(t, found)
	assert.Equal(t, "uuid-short", uuid)
	assert.Equal(t, int64(10), providerID)
	assert.Equal(t, "u:a-m:b", matchedChain)
}

func TestDigestSessionStore_LongestPrefixMatch(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "u:a", "uuid-1", 1, "")
	store.Save(1, "prefix", "u:a-m:b", "uuid-2", 2, "")
	store.Save(1, "prefix", "u:a-m:b-u:c", "uuid-3", 3, "")

	// 应匹配最深的 "u:a-m:b-u:c"（从完整 chain 逐段截断，先命中最长的）
	uuid, providerID, _, found := store.Find(1, "prefix", "u:a-m:b-u:c-m:d-u:e")
	require.True(t, found)
	assert.Equal(t, "uuid-3", uuid)
	assert.Equal(t, int64(3), providerID)

	// 查找中等长度，应匹配到 "u:a-m:b"
	uuid, providerID, _, found = store.Find(1, "prefix", "u:a-m:b-u:x")
	require.True(t, found)
	assert.Equal(t, "uuid-2", uuid)
	assert.Equal(t, int64(2), providerID)
}

func TestDigestSessionStore_SaveDeletesOldChain(t *testing.T) {
	store := NewDigestSessionStore()

	// 第一轮：保存 "u:a-m:b"
	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "")

	// 第二轮：同一 uuid 保存更长的链，传入旧 chain
	store.Save(1, "prefix", "u:a-m:b-u:c-m:d", "uuid-1", 100, "u:a-m:b")

	// 旧链 "u:a-m:b" 应已被删除
	_, _, _, found := store.Find(1, "prefix", "u:a-m:b")
	assert.False(t, found, "old chain should be deleted")

	// 新链应能找到
	uuid, providerID, _, found := store.Find(1, "prefix", "u:a-m:b-u:c-m:d")
	require.True(t, found)
	assert.Equal(t, "uuid-1", uuid)
	assert.Equal(t, int64(100), providerID)
}

func TestDigestSessionStore_DifferentSessionsNoInterference(t *testing.T) {
	store := NewDigestSessionStore()

	// 相同系统提示词，不同用户提示词
	store.Save(1, "prefix", "s:sys-u:user1", "uuid-1", 100, "")
	store.Save(1, "prefix", "s:sys-u:user2", "uuid-2", 200, "")

	uuid, providerID, _, found := store.Find(1, "prefix", "s:sys-u:user1-m:reply1")
	require.True(t, found)
	assert.Equal(t, "uuid-1", uuid)
	assert.Equal(t, int64(100), providerID)

	uuid, providerID, _, found = store.Find(1, "prefix", "s:sys-u:user2-m:reply2")
	require.True(t, found)
	assert.Equal(t, "uuid-2", uuid)
	assert.Equal(t, int64(200), providerID)
}

func TestDigestSessionStore_NoMatch(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "")

	// 完全不同的 chain
	_, _, _, found := store.Find(1, "prefix", "u:x-m:y")
	assert.False(t, found)
}

func TestDigestSessionStore_DifferentPrefixHash(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix1", "u:a-m:b", "uuid-1", 100, "")

	// 不同 prefixHash 应隔离
	_, _, _, found := store.Find(1, "prefix2", "u:a-m:b")
	assert.False(t, found)
}

func TestDigestSessionStore_DifferentGroupID(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "")

	// 不同 groupID 应隔离
	_, _, _, found := store.Find(2, "prefix", "u:a-m:b")
	assert.False(t, found)
}

func TestDigestSessionStore_EmptyDigestChain(t *testing.T) {
	store := NewDigestSessionStore()

	// 空链不应保存
	store.Save(1, "prefix", "", "uuid-1", 100, "")
	_, _, _, found := store.Find(1, "prefix", "")
	assert.False(t, found)
}

func TestDigestSessionStore_TTLExpiration(t *testing.T) {
	store := &DigestSessionStore{
		cache: gocache.New(100*time.Millisecond, 50*time.Millisecond),
	}

	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "")

	// 立即应该能找到
	_, _, _, found := store.Find(1, "prefix", "u:a-m:b")
	require.True(t, found)

	// 等待过期 + 清理周期
	time.Sleep(300 * time.Millisecond)

	// 过期后应找不到
	_, _, _, found = store.Find(1, "prefix", "u:a-m:b")
	assert.False(t, found)
}

func TestDigestSessionStore_ConcurrentSafety(t *testing.T) {
	store := NewDigestSessionStore()

	var wg sync.WaitGroup
	const goroutines = 50
	const operations = 100

	wg.Add(goroutines)
	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			prefix := fmt.Sprintf("prefix-%d", id%5)
			for i := range operations {
				chain := fmt.Sprintf("u:%d-m:%d", id, i)
				uuid := fmt.Sprintf("uuid-%d-%d", id, i)
				store.Save(1, prefix, chain, uuid, int64(id), "")
				store.Find(1, prefix, chain)
			}
		}(g)
	}
	wg.Wait()
}

func TestDigestSessionStore_MultipleSessions(t *testing.T) {
	store := NewDigestSessionStore()

	sessions := []struct {
		chain      string
		uuid       string
		providerID int64
	}{
		{"u:session1", "uuid-1", 1},
		{"u:session2-m:reply2", "uuid-2", 2},
		{"u:session3-m:reply3-u:msg3", "uuid-3", 3},
	}

	for _, sess := range sessions {
		store.Save(1, "prefix", sess.chain, sess.uuid, sess.providerID, "")
	}

	// 验证每个会话都能正确查找
	for _, sess := range sessions {
		uuid, providerID, _, found := store.Find(1, "prefix", sess.chain)
		require.True(t, found, "should find session: %s", sess.chain)
		assert.Equal(t, sess.uuid, uuid)
		assert.Equal(t, sess.providerID, providerID)
	}

	// 验证继续对话的场景
	uuid, providerID, _, found := store.Find(1, "prefix", "u:session2-m:reply2-u:newmsg")
	require.True(t, found)
	assert.Equal(t, "uuid-2", uuid)
	assert.Equal(t, int64(2), providerID)
}

func TestDigestSessionStore_Performance1000Sessions(t *testing.T) {
	store := NewDigestSessionStore()

	// 插入 1000 个会话
	for i := range 1000 {
		chain := fmt.Sprintf("s:sys-u:user%d-m:reply%d", i, i)
		store.Save(1, "prefix", chain, fmt.Sprintf("uuid-%d", i), int64(i), "")
	}

	// 查找性能测试
	start := time.Now()
	const lookups = 10000
	for i := range lookups {
		idx := i % 1000
		chain := fmt.Sprintf("s:sys-u:user%d-m:reply%d-u:newmsg", idx, idx)
		_, _, _, found := store.Find(1, "prefix", chain)
		assert.True(t, found)
	}
	elapsed := time.Since(start)
	t.Logf("%d lookups in %v (%.0f ns/op)", lookups, elapsed, float64(elapsed.Nanoseconds())/lookups)
}

func TestDigestSessionStore_FindReturnsMatchedChain(t *testing.T) {
	store := NewDigestSessionStore()

	store.Save(1, "prefix", "u:a-m:b-u:c", "uuid-1", 100, "")

	// 精确匹配
	_, _, matchedChain, found := store.Find(1, "prefix", "u:a-m:b-u:c")
	require.True(t, found)
	assert.Equal(t, "u:a-m:b-u:c", matchedChain)

	// 前缀匹配（截断后命中）
	_, _, matchedChain, found = store.Find(1, "prefix", "u:a-m:b-u:c-m:d-u:e")
	require.True(t, found)
	assert.Equal(t, "u:a-m:b-u:c", matchedChain)
}

func TestDigestSessionStore_CacheItemCountStable(t *testing.T) {
	store := NewDigestSessionStore()

	// 模拟 100 个独立会话，每个进行 10 轮对话
	// 正确传递 oldDigestChain 时，每个会话始终只保留 1 个 key
	for conv := range 100 {
		var prevMatchedChain string
		for round := range 10 {
			chain := fmt.Sprintf("s:sys-u:user%d", conv)
			for r := range round {
				chain += fmt.Sprintf("-m:a%d-u:q%d", r, r+1)
			}
			uuid := fmt.Sprintf("uuid-conv%d", conv)

			_, _, matched, _ := store.Find(1, "prefix", chain)
			store.Save(1, "prefix", chain, uuid, int64(conv), matched)
			prevMatchedChain = matched
			_ = prevMatchedChain
		}
	}

	// 100 个会话 × 1 key/会话 = 应该 ≤ 100 个 key
	// 允许少量并发残留，但绝不能接近 100×10=1000
	itemCount := store.cache.ItemCount()
	assert.LessOrEqual(t, itemCount, 100, "cache should have at most 100 items (1 per conversation), got %d", itemCount)
	t.Logf("Cache item count after 100 conversations × 10 rounds: %d", itemCount)
}

func TestDigestSessionStore_TTLPreventsUnboundedGrowth(t *testing.T) {
	// 使用极短 TTL 验证大量写入后 cache 能被清理
	store := &DigestSessionStore{
		cache: gocache.New(100*time.Millisecond, 50*time.Millisecond),
	}

	// 插入 500 个不同的 key（无 oldDigestChain，模拟最坏场景：全是新会话首轮）
	for i := range 500 {
		chain := fmt.Sprintf("u:user%d", i)
		store.Save(1, "prefix", chain, fmt.Sprintf("uuid-%d", i), int64(i), "")
	}

	assert.Equal(t, 500, store.cache.ItemCount())

	// 等待 TTL + 清理周期
	time.Sleep(300 * time.Millisecond)

	assert.Equal(t, 0, store.cache.ItemCount(), "all items should be expired and cleaned up")
}

func TestDigestSessionStore_SaveSameChainNoDelete(t *testing.T) {
	store := NewDigestSessionStore()

	// 保存 chain
	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "")

	// 用户重发相同消息：oldDigestChain == digestChain，不应删掉刚设置的 key
	store.Save(1, "prefix", "u:a-m:b", "uuid-1", 100, "u:a-m:b")

	// 仍然能找到
	uuid, providerID, _, found := store.Find(1, "prefix", "u:a-m:b")
	require.True(t, found)
	assert.Equal(t, "uuid-1", uuid)
	assert.Equal(t, int64(100), providerID)
}

// TestGeminiSessionContinuousConversation 测试连续会话的摘要链匹配。
func TestGeminiSessionContinuousConversation(t *testing.T) {
	store := NewDigestSessionStore()
	groupID := int64(1)
	prefixHash := "test_prefix_hash"
	sessionUUID := "session-uuid-12345"
	providerID := int64(100)

	// 模拟第一轮对话
	req1 := &protocolgemini.GeminiRequest{
		SystemInstruction: &protocolgemini.GeminiContent{
			Parts: []protocolgemini.GeminiPart{{Text: "You are a helpful assistant"}},
		},
		Contents: []protocolgemini.GeminiContent{
			{Role: "user", Parts: []protocolgemini.GeminiPart{{Text: "Hello, what's your name?"}}},
		},
	}
	chain1 := BuildGeminiDigestChain(req1)
	t.Logf("Round 1 chain: %s", chain1)

	// 第一轮：没有找到会话，创建新会话
	_, _, _, found := store.Find(groupID, prefixHash, chain1)
	if found {
		t.Error("Round 1: should not find existing session")
	}

	// 保存第一轮会话（首轮无旧 chain）
	store.Save(groupID, prefixHash, chain1, sessionUUID, providerID, "")

	// 模拟第二轮对话（用户继续对话）
	req2 := &protocolgemini.GeminiRequest{
		SystemInstruction: &protocolgemini.GeminiContent{
			Parts: []protocolgemini.GeminiPart{{Text: "You are a helpful assistant"}},
		},
		Contents: []protocolgemini.GeminiContent{
			{Role: "user", Parts: []protocolgemini.GeminiPart{{Text: "Hello, what's your name?"}}},
			{Role: "model", Parts: []protocolgemini.GeminiPart{{Text: "I'm Claude, nice to meet you!"}}},
			{Role: "user", Parts: []protocolgemini.GeminiPart{{Text: "What can you do?"}}},
		},
	}
	chain2 := BuildGeminiDigestChain(req2)
	t.Logf("Round 2 chain: %s", chain2)

	// 第二轮通过第一轮的摘要前缀找到会话。
	foundUUID, foundAccID, matchedChain, found := store.Find(groupID, prefixHash, chain2)
	if !found {
		t.Error("Round 2: should find session via prefix matching")
	}
	if foundUUID != sessionUUID {
		t.Errorf("Round 2: expected UUID %s, got %s", sessionUUID, foundUUID)
	}
	if foundAccID != providerID {
		t.Errorf("Round 2: expected providerID %d, got %d", providerID, foundAccID)
	}

	// 保存第二轮会话，传入 Find 返回的 matchedChain 以删旧 key
	store.Save(groupID, prefixHash, chain2, sessionUUID, providerID, matchedChain)

	// 模拟第三轮对话
	req3 := &protocolgemini.GeminiRequest{
		SystemInstruction: &protocolgemini.GeminiContent{
			Parts: []protocolgemini.GeminiPart{{Text: "You are a helpful assistant"}},
		},
		Contents: []protocolgemini.GeminiContent{
			{Role: "user", Parts: []protocolgemini.GeminiPart{{Text: "Hello, what's your name?"}}},
			{Role: "model", Parts: []protocolgemini.GeminiPart{{Text: "I'm Claude, nice to meet you!"}}},
			{Role: "user", Parts: []protocolgemini.GeminiPart{{Text: "What can you do?"}}},
			{Role: "model", Parts: []protocolgemini.GeminiPart{{Text: "I can help with coding, writing, and more!"}}},
			{Role: "user", Parts: []protocolgemini.GeminiPart{{Text: "Great, help me write some Go code"}}},
		},
	}
	chain3 := BuildGeminiDigestChain(req3)
	t.Logf("Round 3 chain: %s", chain3)

	// 第三轮通过第二轮的摘要前缀找到会话。
	foundUUID, foundAccID, _, found = store.Find(groupID, prefixHash, chain3)
	if !found {
		t.Error("Round 3: should find session via prefix matching")
	}
	if foundUUID != sessionUUID {
		t.Errorf("Round 3: expected UUID %s, got %s", sessionUUID, foundUUID)
	}
	if foundAccID != providerID {
		t.Errorf("Round 3: expected providerID %d, got %d", providerID, foundAccID)
	}
}

// TestGeminiSessionDifferentConversations 测试不同会话不会错误匹配。
func TestGeminiSessionDifferentConversations(t *testing.T) {
	store := NewDigestSessionStore()
	groupID := int64(1)
	prefixHash := "test_prefix_hash"

	// 第一个会话
	req1 := &protocolgemini.GeminiRequest{
		Contents: []protocolgemini.GeminiContent{
			{Role: "user", Parts: []protocolgemini.GeminiPart{{Text: "Tell me about Go programming"}}},
		},
	}
	chain1 := BuildGeminiDigestChain(req1)
	store.Save(groupID, prefixHash, chain1, "session-1", 100, "")

	// 另一个会话使用不同的用户消息。
	req2 := &protocolgemini.GeminiRequest{
		Contents: []protocolgemini.GeminiContent{
			{Role: "user", Parts: []protocolgemini.GeminiPart{{Text: "What's the weather today?"}}},
		},
	}
	chain2 := BuildGeminiDigestChain(req2)

	// 不同会话不应该匹配
	_, _, _, found := store.Find(groupID, prefixHash, chain2)
	if found {
		t.Error("Different conversations should not match")
	}
}

// TestGeminiSessionPrefixMatchingOrder 测试前缀匹配的优先级（最长匹配优先）。
func TestGeminiSessionPrefixMatchingOrder(t *testing.T) {
	store := NewDigestSessionStore()
	groupID := int64(1)
	prefixHash := "test_prefix_hash"

	// 保存不同轮次的会话到不同提供商
	store.Save(groupID, prefixHash, "s:sys-u:q1", "session-round1", 1, "")
	store.Save(groupID, prefixHash, "s:sys-u:q1-m:a1", "session-round2", 2, "")
	store.Save(groupID, prefixHash, "s:sys-u:q1-m:a1-u:q2", "session-round3", 3, "")

	// 更长的摘要链应匹配提供商 3 对应的最长前缀。
	_, accID, _, found := store.Find(groupID, prefixHash, "s:sys-u:q1-m:a1-u:q2-m:a2")
	if !found {
		t.Error("Should find session")
	}
	if accID != 3 {
		t.Errorf("Should match longest prefix (provider 3), got provider %d", accID)
	}
}
