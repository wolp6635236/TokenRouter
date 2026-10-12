package session

import (
	"net/textproto"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

// QoderExplicitSessionSeed 按列表顺序读取 Header 的首值，均为空时再读取请求体中的会话标识。
func QoderExplicitSessionSeed(headers map[string][]string, body []byte) string {
	for _, name := range []string{"session_id", "conversation_id", "x-session-id", "x-conversation-id", "X-Claude-Code-Session-Id"} {
		values := headers[textproto.CanonicalMIMEHeaderKey(name)]
		if len(values) > 0 {
			if value := strings.TrimSpace(values[0]); value != "" {
				return value
			}
		}
	}
	for _, path := range []string{"session_id", "conversation_id", "previous_response_id", "prompt_cache_key"} {
		if value := strings.TrimSpace(gjson.GetBytes(body, path).String()); value != "" {
			return value
		}
	}
	return ""
}

// QoderHashFromSeed 为种子加 qoder: 前缀后计算调度会话哈希。
func QoderHashFromSeed(seed string) string {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return ""
	}
	hash, _ := scheduler.DeriveSessionHashes("qoder:" + seed)
	return hash
}

// QoderRequestHash 优先使用请求指定的会话种子，缺失时根据消息摘要和来源计算哈希。
func QoderRequestHash(headers map[string][]string, body []byte, wire string, identity *requeststate.SessionContext, observe func(string, ...any)) string {
	if seed := QoderExplicitSessionSeed(headers, body); seed != "" {
		return QoderHashFromSeed(seed)
	}
	parsed, err := requeststate.ParseGatewayRequest(requeststate.NewRequestBodyRef(body), wire)
	if err != nil {
		return ""
	}
	parsed.SessionContext = identity
	generated := GenerateSessionHash(parsed, observe)
	if generated == "" {
		return ""
	}
	return QoderHashFromSeed("fallback:" + generated)
}
