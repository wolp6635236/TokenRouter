package httpapi

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

const (
	// invalid_encrypted_content 表示上游拒绝的加密 reasoning 或 compaction 项。
	// 客户端会话历史可能反复携带这些项，每轮都会触发拒绝、剥离和重试。
	// OpenAIWSStateStore 按会话保存 encrypted_content 摘要，并设置 TTL 和容量限制。
	// 后续请求剥离命中摘要的项，新密文使用不同摘要。

	OpenAIInvalidEncryptedContentReason = "invalid_encrypted_content"

	// OpenAIWSIngressLineageContextKey 在 gin context 中携带 ingress 会话哈希，
	// 供 HTTP bridge turn 内的 lineage 记录复用同一会话键。
	OpenAIWSIngressLineageContextKey = "openai_ws_ingress_session_hash"
)

// OpenAIEncryptedLineage 通过 HTTP 与 WS 共用的会话存储读写失效密文。
type OpenAIEncryptedLineage struct {
	Store session.OpenAIWSStateStore
	TTL   func() time.Duration
}

// Mark 把本次被上游拒绝的密文摘要写入
// 会话 lineage。digests 须在剥离前收集。
func (s *OpenAIEncryptedLineage) Mark(groupID int64, sessionHash string, digests []string) {
	if s == nil || len(digests) == 0 || strings.TrimSpace(sessionHash) == "" {
		return
	}
	stateStore := s.Store
	if stateStore == nil {
		return
	}
	stateStore.MarkSessionInvalidEncryptedContent(groupID, sessionHash, digests, s.TTL())
}

// Digests 返回会话已知失效密文摘要；全局无记录
// 时（常态）零成本返回 nil。
func (s *OpenAIEncryptedLineage) Digests(groupID int64, sessionHash string) map[string]struct{} {
	if s == nil || strings.TrimSpace(sessionHash) == "" {
		return nil
	}
	stateStore := s.Store
	if stateStore == nil || !stateStore.HasAnySessionInvalidEncryptedContent() {
		return nil
	}
	return stateStore.GetSessionInvalidEncryptedContentDigests(groupID, sessionHash)
}

// SessionHash 取 lineage 会话键：优先 ingress 循环
// 写入的会话哈希（与读取侧同键），否则按请求体派生。
func (s *OpenAIEncryptedLineage) SessionHash(c *gin.Context, body []byte) string {
	if c != nil {
		if fromCtx := strings.TrimSpace(c.GetString(OpenAIWSIngressLineageContextKey)); fromCtx != "" {
			return fromCtx
		}
	}
	return GenerateOpenAISessionHash(c, body)
}

// MarkPayload 在上游以
// invalid_encrypted_content 拒绝 payload 时记录其密文摘要并输出观测日志。
func (s *OpenAIEncryptedLineage) MarkPayload(
	c *gin.Context,
	payload []byte,
	logKey string,
	providerID int64,
	turn int,
) {
	digests := openai.CollectOpenAIEncryptedContentDigestsRaw(payload)
	if len(digests) == 0 {
		return
	}
	s.Mark(
		OpenAIResponseGroupID(c),
		s.SessionHash(c, payload),
		digests,
	)
	gatewayprovider.LogOpenAIWSModeInfo("%s provider_id=%d turn=%d digests=%d", logKey, providerID, turn, len(digests))
}

// Strip 对 payload 执行会话失效密文剥离并
// 输出观测日志（logKey / logKey+"_skip"），返回（可能已替换的）payload 与剥离
// 项数；未命中或剥离失败时原样返回。
func (s *OpenAIEncryptedLineage) Strip(
	payload []byte,
	invalid map[string]struct{},
	logKey string,
	providerID int64,
	turn int,
) ([]byte, int) {
	strippedPayload, strippedCount, stripErr := openai.StripOpenAIInvalidEncryptedContentRaw(payload, invalid)
	if stripErr != nil {
		gatewayprovider.LogOpenAIWSModeInfo(
			"%s_skip provider_id=%d turn=%d reason=strip_error cause=%s",
			logKey,
			providerID,
			turn, gatewayprovider.TruncateOpenAIWSLogValue(stripErr.Error(), gatewayprovider.OpenAIWSLogValueMaxLen),
		)
		return payload, 0
	}
	if strippedCount > 0 {
		gatewayprovider.LogOpenAIWSModeInfo(
			"%s provider_id=%d turn=%d stripped_items=%d",
			logKey,
			providerID,
			turn,
			strippedCount,
		)
	}
	return strippedPayload, strippedCount
}
