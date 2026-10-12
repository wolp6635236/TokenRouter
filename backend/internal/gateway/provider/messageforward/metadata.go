package messageforward

import (
	"context"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/requeststate"
	"github.com/TokenFlux/TokenRouter/internal/upstream"
	claude "github.com/TokenFlux/TokenRouter/internal/upstream/anthropic"
)

// metadataUserID 保留提供商与客户端会话共同派生的 OAuth 身份。
func metadataUserID(parsed *requeststate.ParsedRequest, provider *gatewayprovider.ExecutionProvider, fp *claude.Fingerprint) string {
	if parsed == nil || provider == nil {
		return ""
	}
	if parsed.MetadataUserID != "" {
		return ""
	}

	userID := strings.TrimSpace(provider.View().GetClaudeUserID())
	if userID == "" && fp != nil {
		userID = fp.ClientID
	}
	if userID == "" {
		// 提供商元数据不完整时生成格式有效的客户端 ID。
		userID = claude.GenerateClientID()
	}

	// session_id 根据提供商、客户端区分因子和首条 user 文本派生，追加消息后保持不变。
	// GenerateSessionHash 用于粘性路由，值会随对话轮次变化。
	var firstUserText string
	if parsed.Body != nil {
		firstUserText = claude.ExtractFirstUserText(parsed.Body.Bytes())
	}
	seed := claude.BuildStableSessionSeed(provider.Record.ID, sessionContextDiscriminator(parsed.SessionContext), firstUserText)
	sessionID := upstream.GenerateSessionUUID(seed)

	// 根据指纹 UA 版本选择输出格式
	var uaVersion string
	if fp != nil {
		uaVersion = claude.ExtractCLIVersion(fp.UserAgent)
	}
	providerUUID := strings.TrimSpace(provider.View().GetExtraString("account_uuid"))
	return claude.FormatMetadataUserID(userID, providerUUID, sessionID, uaVersion)
}

func metadataUserIDFromBody(
	ctx context.Context,
	provider *gatewayprovider.ExecutionProvider,
	fp *claude.Fingerprint,
	body []byte,
) string {
	_ = ctx
	if provider == nil {
		return ""
	}
	if existing := gjson.GetBytes(body, "metadata.user_id").String(); existing != "" {
		return ""
	}

	userID := strings.TrimSpace(provider.View().GetClaudeUserID())
	if userID == "" && fp != nil {
		userID = fp.ClientID
	}
	if userID == "" {
		userID = claude.GenerateClientID()
	}

	// 与 buildOAuthMetadataUserID 一致：用会话级稳定种子，避免整 body 哈希导致
	// 每轮（甚至每个 token 变化）都重算出不同的 session_id。
	var clientDiscriminator string
	if fp != nil {
		clientDiscriminator = fp.ClientID
	}
	seed := claude.BuildStableSessionSeed(provider.Record.ID, clientDiscriminator, claude.ExtractFirstUserText(body))
	sessionID := upstream.GenerateSessionUUID(seed)

	var uaVersion string
	if fp != nil {
		uaVersion = claude.ExtractCLIVersion(fp.UserAgent)
	}
	providerUUID := strings.TrimSpace(provider.View().GetExtraString("account_uuid"))
	return claude.FormatMetadataUserID(userID, providerUUID, sessionID, uaVersion)
}

// sessionContextDiscriminator 把请求上下文（客户端 IP / 归一化 UA / API Key ID）拼成
// 一个跨客户端的区分因子，避免不同用户的相同首条消息派生出相同 session_id。
func sessionContextDiscriminator(sc *requeststate.SessionContext) string {
	if sc == nil {
		return ""
	}
	return sc.ClientIP + ":" + requeststate.NormalizeSessionUserAgent(sc.UserAgent) + ":" + strconv.FormatInt(sc.APIKeyID, 10)
}
