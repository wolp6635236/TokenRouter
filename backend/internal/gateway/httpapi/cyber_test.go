package httpapi

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/gateway/moderationflow"
	"github.com/TokenFlux/TokenRouter/internal/identity/httpapi/authctx"
)

func newCyberTest(p *cyberTestPorts) *CyberHandler {
	return NewCyberHandler(p, p, p, moderationflow.Runtime{Tasks: p, Blocks: p, Ops: p})
}

func TestCyberPolicyScopeDedupAndBackgroundOrder(t *testing.T) {
	p := &cyberTestPorts{scope: true, mark: moderationflow.Mark{Message: "warning", Body: "original", UpstreamStatus: 403}}
	h := newCyberTest(p)
	c, _ := prefaceContext("body")
	call := CyberPolicyCall{Key: prefaceKey(), Model: "model", Plan: moderationflow.BlockPlan{ScopeKey: "scope", Keys: []string{"key"}}, HasPlan: true}
	require.True(t, h.RecordPolicy(c, call))
	require.Equal(t, []string{"block", "scope", "warning", "submit"}, p.events)
	require.True(t, c.GetBool(CyberPolicyRecordedKey))
	require.True(t, c.GetBool(CyberWarningRecordedKey))
	p.mark.Body = "changed"
	call.Model = "changed"
	require.True(t, h.RecordPolicy(c, call))
	require.Equal(t, []string{"block", "scope", "warning", "submit", "block"}, p.events)
	p.task()
	require.Equal(t, "original", p.entry.ErrorBody)
	require.Equal(t, "model", p.entry.Model)
	require.Equal(t, []string{"block", "ops"}, p.events[len(p.events)-2:])
}

func TestCyberPolicyOutOfScopeRetainsOnlyEarlySessionWrite(t *testing.T) {
	for _, scopeErr := range []error{nil, errors.New("scope unavailable")} {
		p := &cyberTestPorts{scopeErr: scopeErr}
		h := newCyberTest(p)
		c, _ := prefaceContext("body")
		require.False(t, h.RecordPolicy(c, CyberPolicyCall{Key: prefaceKey(), HasPlan: true, Plan: moderationflow.BlockPlan{Keys: []string{"key"}}}))
		require.Equal(t, []string{"block", "scope"}, p.events)
		require.False(t, c.GetBool(CyberPolicyRecordedKey))
		require.Nil(t, p.task)
	}
}

func TestCyberSessionBlockPreservesScopeAndDedicatedOps(t *testing.T) {
	p := &cyberTestPorts{enabled: true, scope: true, found: "blocked"}
	c, w := prefaceContext("body")
	require.True(t, newCyberTest(p).RejectSession(c, prefaceKey(), nil, "model", CyberBlockChat))
	require.Equal(t, 403, w.Code)
	require.Contains(t, w.Body.String(), "session_blocked_by_cyber_policy")
	require.Equal(t, []string{"enabled", "group", "find", "ops"}, p.events)
	require.True(t, c.GetBool("ops_dedicated_error_recorded"))
	p = &cyberTestPorts{enabled: true, scope: false}
	c, _ = prefaceContext("body")
	require.False(t, newCyberTest(p).RejectSession(c, prefaceKey(), nil, "model", CyberBlockResponses))
	require.Equal(t, []string{"enabled", "group"}, p.events)
}

func TestOrdinaryModerationFailureRemainsFailOpen(t *testing.T) {
	p := &cyberTestPorts{}
	c, _ := prefaceContext("body")
	require.Nil(t, RunContentModeration(p, c, nil, p, prefaceKey(), authctx.AuthSubject{UserID: 42}, "openai_responses", "m", []byte("body")))
	require.Empty(t, p.events)
}
