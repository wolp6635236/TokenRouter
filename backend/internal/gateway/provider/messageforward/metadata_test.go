package messageforward

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"
	"github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
)

func TestBuildOAuthMetadataUserID_FallbackWithoutAccountUUID(t *testing.T) {
	parsed := &requeststate.ParsedRequest{
		Model:          "claude-sonnet-4-5",
		Stream:         true,
		MetadataUserID: "",
	}

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 123,
			Type:  capability.ProviderTypeOAuth,
			Extra: map[string]any{},
		}, // 刻意省略提供商与客户端标识
	}

	fp := &anthropic.Fingerprint{ClientID: "deadbeef"} // 旧格式使用此客户端标识

	got := metadataUserID(parsed, provider, fp)
	require.NotEmpty(t, got)

	// 旧格式在提供商标识缺失时保留空字段。
	re := regexp.MustCompile(`^user_[a-zA-Z0-9]+_account__session_[a-f0-9-]{36}$`)
	require.True(t, re.MatchString(got), "unexpected user_id format: %s", got)
}

func TestBuildOAuthMetadataUserID_UsesAccountUUIDWhenPresent(t *testing.T) {
	parsed := &requeststate.ParsedRequest{
		Model:          "claude-sonnet-4-5",
		Stream:         true,
		MetadataUserID: "",
	}

	provider := &gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 123,
			Type: capability.ProviderTypeOAuth,
			Extra: map[string]any{
				"account_uuid":      "acc-uuid",
				"claude_user_id":    "clientid123",
				"anthropic_user_id": "",
			},
		},
	}

	got := metadataUserID(parsed, provider, nil)
	require.NotEmpty(t, got)

	// 提供商标识存在时写入完整身份字段。
	re := regexp.MustCompile(`^user_clientid123_account_acc-uuid_session_[a-f0-9-]{36}$`)
	require.True(t, re.MatchString(got), "unexpected user_id format: %s", got)
}

// TestBuildOAuthMetadataUserID_SessionIDStableAcrossTurns 验证伪装路径合成的
// metadata.user_id 在同一会话多轮请求间保持不变（session_id 稳定），贴近真实 Claude Code
// 进程级稳定的 session。提供商 / 指纹 / UA 版本均相同，唯一可能变化的就是 session_id，
// 因此直接比较完整 user_id 字符串即可判定 session_id 是否稳定。
func TestBuildOAuthMetadataUserID_SessionIDStableAcrossTurns(t *testing.T) {
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, ID: 777, Type: capability.ProviderTypeOAuth, Extra: map[string]any{"account_uuid": "acc-uuid"}}}
	fp := &anthropic.Fingerprint{ClientID: "clientid777", UserAgent: "claude-cli/2.1.161 (external, cli)"}

	mustParse := func(body string) *requeststate.ParsedRequest {
		parsed, err := requeststate.ParseGatewayRequest(requeststate.NewRequestBodyRef([]byte(body)), capability.PlatformAnthropic)
		require.NoError(t, err)
		return parsed
	}

	round1 := mustParse(`{"model":"claude-sonnet-4-5","system":"sys","messages":[` +
		`{"role":"user","content":"first question"}]}`)
	round2 := mustParse(`{"model":"claude-sonnet-4-5","system":"sys","messages":[` +
		`{"role":"user","content":"first question"},` +
		`{"role":"assistant","content":"answer 1"},` +
		`{"role":"user","content":"second question"}]}`)
	round3 := mustParse(`{"model":"claude-sonnet-4-5","system":"sys","messages":[` +
		`{"role":"user","content":"first question"},` +
		`{"role":"assistant","content":"answer 1"},` +
		`{"role":"user","content":"second question"},` +
		`{"role":"assistant","content":"answer 2"},` +
		`{"role":"user","content":"third question"}]}`)

	id1 := metadataUserID(round1, provider, fp)
	id2 := metadataUserID(round2, provider, fp)
	id3 := metadataUserID(round3, provider, fp)

	require.NotEmpty(t, id1)
	require.Equal(t, id1, id2, "session_id 应随对话增长保持不变")
	require.Equal(t, id2, id3, "session_id 应跨所有轮次保持不变")

	// 不同的首条 user 消息应派生出不同的 session_id（不同会话）。
	other := mustParse(`{"model":"claude-sonnet-4-5","system":"sys","messages":[` +
		`{"role":"user","content":"a completely different opener"}]}`)
	idOther := metadataUserID(other, provider, fp)
	require.NotEqual(t, id1, idOther, "不同首条消息应派生不同 session_id")
}
