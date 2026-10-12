package httpapi

import (
	"context"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/egress"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/gateway/promptpolicy"
	gatewayadapter "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	gatewayws "github.com/TokenFlux/TokenRouter/internal/gateway/ws"
	openaiws "github.com/TokenFlux/TokenRouter/internal/upstream/openai/ws"
)

// OpenAIWSOptions 保存 WS 执行的静态参数，nil 表示使用默认配置。
type OpenAIWSOptions = gatewayws.Parameters

// OpenAIWSSelection 只提供已经选中提供商的传输选择与会话预算。
type OpenAIWSSelection interface {
	ResolveTransport(*gatewayadapter.ExecutionProvider) egress.OpenAIWSProtocolDecision
	SessionStickyTTL() time.Duration
}

// OpenAIWSDependencies 保存 WS 执行使用的接口。
type OpenAIWSDependencies struct {
	Options     *OpenAIWSOptions
	Runtime     *gatewayws.Runtime
	Connections *openaiws.OpenAIWSConnections
	Requests    *gatewayhttp.OpenAIRequests
	Output      *gatewayhttp.OpenAIResponseOutput
	Grok        *gatewayhttp.GrokExecutor
	FastPolicy  *gatewayadapter.ExecutionFastPolicy
	Prompts     *promptpolicy.Service
	Selection   OpenAIWSSelection
	State       session.OpenAIWSStateStore
	Lineage     *gatewayhttp.OpenAIEncryptedLineage
	ImageBridge *gatewayadapter.ResponseImagePolicy
	Cache       session.GatewayCache
}

// OpenAIWebSocketExecutor 适配帧、凭据、健康与 HTTP 桥接，逐轮循环由 gateway/ws 唯一拥有。
type OpenAIWebSocketExecutor struct {
	OpenAIWSDependencies
	openaiWSSessionPreemptions openAIWSSessionPreemptRegistry
}

// NewOpenAIWebSocketExecutor 构造不启动连接或会话任务。
func NewOpenAIWebSocketExecutor(deps OpenAIWSDependencies) *OpenAIWebSocketExecutor {
	out := &OpenAIWebSocketExecutor{OpenAIWSDependencies: deps}

	return out
}

// EnsureSessionIsolation 使用当前请求的认证 Key 检查会话隔离。
func (s *OpenAIWebSocketExecutor) EnsureSessionIsolation(ctx context.Context, key *apikey.APIKey, userID int64, source, hash string) error {
	if key == nil {
		return nil
	}
	var group int64
	if key.GroupID != nil {
		group = *key.GroupID
	}
	return session.EnsureIsolation(ctx, s.Cache, session.IsolationInput{UserID: userID, GroupID: group, Source: source, Hash: hash, TTL: time.Hour, Enabled: key.Group != nil && key.Group.SessionIsolationEnabled})
}

// forTurn 为当前轮次冻结参数，其他轮次可以读取后续发布的值。
func (s *OpenAIWebSocketExecutor) forTurn() *OpenAIWebSocketExecutor {
	if s.Runtime == nil {
		return s
	}
	deps := s.OpenAIWSDependencies
	value := s.Runtime.Snapshot()
	deps.Options = &value
	return &OpenAIWebSocketExecutor{OpenAIWSDependencies: deps}
}
