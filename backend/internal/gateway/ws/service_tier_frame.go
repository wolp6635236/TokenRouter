package ws

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/gateway/tierpolicy"
)

// ApplyServiceTierFrame 对发往上游的 response.create 帧应用与 HTTP 相同的 OpenAI Fast 策略。
// pass 保留档位并将 fast 规范化为 priority，filter 删除顶层 service_tier，force_priority 写入 priority，block 返回策略错误。
// 所有帧先校验 Ultra，其他类型（包括空 type）通过该校验后原样转发，空 type 由上游拒绝。
// service_tier 位于帧顶层，与 Responses HTTP 请求一致，调用方传入本轮上游模型。
func ApplyServiceTierFrame(frame []byte, model string, input tierpolicy.DecisionInput) ([]byte, *tierpolicy.BlockedError, error) {
	if len(frame) == 0 {
		return frame, nil, nil
	}
	if !gjson.ValidBytes(frame) {
		return frame, nil, nil
	}
	// WS 会话允许逐帧切换参数，因此每个客户端帧都必须在上游转发前拒绝 Ultra。
	if err := requeststate.ValidateOpenAIReasoningEffort(frame, model); err != nil {
		return frame, nil, err
	}
	frameType := strings.TrimSpace(gjson.GetBytes(frame, "type").String())
	// response.create 执行 Fast 策略检查，其他事件原样转发。Realtime 要求 type，缺失时由上游拒绝。
	if frameType != "response.create" {
		return frame, nil, nil
	}
	tierResult := gjson.GetBytes(frame, "service_tier")
	decision := tierpolicy.Resolve(input, tierResult.String(), tierResult.Exists())
	if decision.Blocked != nil {
		return frame, decision.Blocked, nil
	}
	if decision.DeleteField {
		trimmed, err := sjson.DeleteBytes(frame, "service_tier")
		if err != nil {
			return frame, nil, fmt.Errorf("strip service_tier from ws frame: %w", err)
		}
		return trimmed, nil, nil
	}
	if decision.Tier != "" && (!tierResult.Exists() || decision.Tier != tierResult.String()) {
		updated, err := sjson.SetBytes(frame, "service_tier", decision.Tier)
		if err != nil {
			return frame, nil, fmt.Errorf("apply service_tier in ws frame: %w", err)
		}
		return updated, nil, nil
	}
	return frame, nil, nil
}

// newOpenAIFastPolicyWSEventID returns a Realtime-style event_id for a
// server-emitted error event. Matches the loose "evt_<rand>" convention used
// by upstream Realtime servers; the exact value is not load-bearing and is
// only required for client-side log correlation. We reuse the existing
// google/uuid dependency rather than pulling a new one.
func newOpenAIFastPolicyWSEventID() string {
	id, err := uuid.NewRandom()
	if err != nil {
		// Extremely unlikely; fall back to a fixed prefix so the field is
		// still non-empty and the schema stays self-consistent.
		return "evt_openai_fast_policy"
	}
	// Strip dashes so it visually matches "evt_<hex>" rather than UUID v4
	// canonical form, mirroring what real Realtime traces look like.
	return "evt_" + strings.ReplaceAll(id.String(), "-", "")
}

// BuildFastPolicyBlockedEvent renders an OpenAI Realtime/Responses
// style "error" event payload for a request blocked by the OpenAI fast
// policy. The shape mirrors Realtime error events as observed in upstream
// traces and per the spec's server "error" event:
//
//	{
//	  "event_id": "evt_<random>",
//	  "type": "error",
//	  "error": {
//	    "type": "invalid_request_error",
//	    "code": "policy_violation",
//	    "message": "..."
//	  }
//	}
//
// event_id lets clients correlate the rejection in their logs; "code" gives
// programmatic clients a stable identifier (HTTP-side equivalent is the
// 403 permission_error JSON body).
func BuildFastPolicyBlockedEvent(err *tierpolicy.BlockedError) []byte {
	if err == nil {
		return nil
	}
	eventID := newOpenAIFastPolicyWSEventID()
	payload, mErr := json.Marshal(map[string]any{
		"event_id": eventID,
		"type":     "error",
		"error": map[string]any{
			"type":    "invalid_request_error",
			"code":    "policy_violation",
			"message": err.Message,
		},
	})
	if mErr != nil {
		// Fallback to a minimal hand-rolled payload; Marshal of the literal
		// shape above should never fail in practice.
		return []byte(`{"event_id":"` + eventID + `","type":"error","error":{"type":"invalid_request_error","code":"policy_violation","message":"openai fast policy blocked this request"}}`)
	}
	return payload
}
