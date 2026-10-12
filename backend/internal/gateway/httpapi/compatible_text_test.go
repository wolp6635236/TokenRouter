package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/moderation"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
)

func TestCompatiblePrefaceOrderingAndEnvelope(t *testing.T) {
	for _, responses := range []bool{true, false} {
		name, protocol, field := "chat", "chat_completions", "type"
		if responses {
			name, protocol, field = "responses", "openai_responses", "code"
		}
		t.Run(name, func(t *testing.T) {
			p := &prefaceBackend{key: prefaceKey(), block: true}
			h := NewCompatibleTextHandler(MessagesHTTPOptions{MaxBodyBytes: 1024}, p, p, nil, p)
			c, w := prefaceContext(`{"model":"original","stream":true}`)
			if responses {
				h.Responses(c)
			} else {
				h.ChatCompletions(c)
			}
			prefix := []string{"request:", "prompt:" + protocol, "reasoning"}
			if responses {
				prefix = append(prefix, "request:original", "endpoint", "plan", "bind", "image")
			} else {
				prefix = append(prefix, "plan", "bind", "image", "request:original", "endpoint")
			}
			moderationProtocol := moderation.ContentModerationProtocolOpenAIChat
			if responses {
				moderationProtocol = moderation.ContentModerationProtocolOpenAIResponses
			}
			require.Equal(t, append(prefix, "moderate:"+moderationProtocol), p.events)
			require.Contains(t, w.Body.String(), `"`+field+`":"content_policy_violation"`)
			require.NotEqual(t, http.StatusOK, w.Code)
		})
	}
}

func TestCompatiblePrefaceValidationBeforeScheduling(t *testing.T) {
	for _, tc := range []struct {
		name, body, message string
		limit               int64
		policy              bool
	}{
		{"empty", "", "Request body is empty", 1024, false},
		{"json", "{", "Failed to parse request body", 1024, false},
		{"model", `{"input":"text"}`, "model is required", 1024, false},
		{"stream", `{"model":"m","stream":"true"}`, InvalidStreamFieldTypeMessage, 1024, false},
		{"policy", `{"model":"m","stream":"true"}`, "policy rejected", 1024, true},
		{"limit", `{"model":"m"}`, "Request body too large, limit is 4B", 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, responses := range []bool{true, false} {
				p := &prefaceBackend{key: prefaceKey()}
				if tc.policy {
					p.policyErr = errors.New("policy rejected")
				}
				h := NewCompatibleTextHandler(MessagesHTTPOptions{MaxBodyBytes: tc.limit}, p, p, nil, p)
				c, w := prefaceContext(tc.body)
				if responses {
					h.Responses(c)
				} else {
					h.ChatCompletions(c)
				}
				require.Contains(t, w.Body.String(), tc.message)
				require.NotContains(t, p.events, "plan")
			}
		})
	}
}

func TestCompatiblePrefacePassesRequestLeaseAndOriginalModel(t *testing.T) {
	for _, responses := range []bool{true, false} {
		p := &prefaceBackend{key: prefaceKey()}
		h := NewCompatibleTextHandler(MessagesHTTPOptions{MaxBodyBytes: 1024}, p, p, prefaceConcurrency(), p)
		c, w := prefaceContext(`{"model":"original","input":"hello","messages":[{"role":"user","content":"hi"}]}`)
		if responses {
			h.Responses(c)
		} else {
			h.ChatCompletions(c)
		}
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, "original", p.call.Model)
		require.Equal(t, "mapped-model", p.call.Mapping.MappedModel)
		require.NotNil(t, scheduler.RequestLease(p.call.RequestContext))
		require.Equal(t, []string{"eligibility", "execution"}, p.events[len(p.events)-2:])
	}
}
