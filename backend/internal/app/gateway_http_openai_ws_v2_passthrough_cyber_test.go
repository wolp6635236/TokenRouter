package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	keyhttp "github.com/TokenFlux/TokenRouter/internal/apikey/httpapi"
	billingtestkit "github.com/TokenFlux/TokenRouter/internal/billing/testkit"
	httptestkit "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi/testkit"
	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	gatewaytestkit "github.com/TokenFlux/TokenRouter/internal/gateway/testkit"
	"github.com/TokenFlux/TokenRouter/internal/identity/httpapi/authctx"
	"github.com/TokenFlux/TokenRouter/internal/infra/telemetry/logging"
	"github.com/TokenFlux/TokenRouter/internal/scheduler"
	"github.com/TokenFlux/TokenRouter/internal/usage"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/apikey/testkit"
	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/routing"

	"github.com/TokenFlux/TokenRouter/internal/identity"
	"github.com/TokenFlux/TokenRouter/internal/routing/capability"

	"github.com/TokenFlux/TokenRouter/internal/config"
	gatewayhttp "github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/gateway/session"
	"github.com/TokenFlux/TokenRouter/internal/moderation"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
	"github.com/TokenFlux/TokenRouter/internal/testutil"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type openAIWSPassthroughHandlerHarness struct {
	clientConn     *coderws.Conn
	handlerDone    <-chan struct{}
	moderationRepo *contentModerationHandlerTestRepo
	gatewayCache   session.GatewayCache
	apiKey         *apikey.APIKey
	keys           *wsTurnKeys
}

// wsTurnKeys 独立保存当前认证记录，测试中的删除不会修改连接已持有的快照。
type wsTurnKeys struct {
	apikey.APIKeyRepository
	mu  sync.Mutex
	key *apikey.APIKey
}

func (r *wsTurnKeys) GetByKeyForAuth(context.Context, string) (*apikey.APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.key == nil {
		return nil, apikey.ErrAPIKeyNotFound
	}
	return apikey.CopyAPIKey(r.key), nil
}

func (r *wsTurnKeys) remove() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.key = nil
}

func (r *contentModerationHandlerTestRepo) cyberWarningSnapshot() []moderation.ContentModerationCyberWarning {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]moderation.ContentModerationCyberWarning(nil), r.cyberWarnings...)
}

func newOpenAIWSPassthroughHandlerHarness(t *testing.T, upstreamURL string) *openAIWSPassthroughHandlerHarness {
	t.Helper()
	gatewayCache := testutil.NewRedisGatewayCache(t)

	settingRepo := &contentModerationHandlerSettingRepo{values: map[string]string{
		moderation.SettingKeyRiskControlEnabled:          "true",
		moderation.SettingKeyCyberSessionBlockEnabled:    "true",
		moderation.SettingKeyCyberSessionBlockTTLSeconds: "60",
		moderation.SettingKeyContentModerationConfig:     `{"enabled":true,"mode":"observe","cyber_warning_enabled":true,"all_groups":true}`,
	}}
	moderationRepo := &contentModerationHandlerTestRepo{}
	moderationSvc := newHTTPModeration(t, settingRepo, moderationRepo)
	moderationSvc.Start()
	settingSvc := gatewaytestkit.RuntimeReaders(settingRepo)

	groupID := int64(4301)
	provider := gatewayprovider.ExecutionProvider{
		Record: providercore.Record{
			LoadLocation: time.LoadLocation, ID: 9951,
			Name:        "openai-ws-passthrough-cyber",
			Platform:    capability.PlatformOpenAI,
			Type:        capability.ProviderTypeAPIKey,
			Status:      billing.StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Credentials: map[string]any{"api_key": "sk-test", "base_url": upstreamURL},
			Extra: map[string]any{
				"openai_apikey_responses_websockets_v2_enabled": true,
				"openai_apikey_responses_websockets_v2_mode":    providercore.OpenAIWSIngressModePassthrough,
			},
		},
	}
	cfg := &config.Config{}

	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3

	providerRepo := &openAIWSUsageHandlerProviderRepoStub{provider: provider}
	usageRepo := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *usage.UsageLog, 2)}
	billingCacheSvc := newBillingEligibilityFixture(cfg)
	billingCacheSvc.Start()
	completionInput16 := billingtestkit.Calculator(nil, nil)
	completionInput17 := &providercore.DeferredService{}
	gatewaySvc, gatewaySvcChoices, gatewaySvcCredentialPort := newOpenAIExecutionAndSelectionFixture(
		providerRepo, gatewayCache, cfg, nil, nil, nil, nil, nil, completionInput17, newOpenAIExecutionCredentialsForTest(providerRepo, nil), nil, nil, nil, settingSvc, nil, responseHeaderFilterForTest(cfg), nil, nil, nil,
	)
	gatewaySvc.Recorder = newHTTPCompletionFixture(cfg, usageRepo, completionInput16, billingCacheSvc, completionInput17, nil, nil, true)

	concurrencyCache := &httptestkit.ConcurrencyHooks{
		AcquireUserSlotFn:     func(context.Context, int64, int, string) (bool, error) { return true, nil },
		AcquireProviderSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	apiKey := &apikey.APIKey{
		ID:      1851,
		UserID:  1751,
		Status:  "active",
		Name:    "ws-cyber-key",
		Key:     "sk-handler-cyber-test",
		GroupID: &groupID,
		User:    &identity.User{ID: 1751, Status: billing.StatusActive},
	}
	keys := &wsTurnKeys{key: apikey.CopyAPIKey(apiKey)}
	keyService := testkit.NewService(keys, nil, fallbackGroupRepository{group: &routing.Group{ID: groupID, Status: "active"}}, nil, nil, nil, nil)
	h := newGatewayHTTPEndpoints(gatewayHTTPFixtureInput{
		Source: gatewaySvc, Credentials: gatewaySvcCredentialPort,
		Funding:     newFundingAdmissionFixture(billingCacheSvc, cfg),
		Keys:        keyService,
		Moderator:   moderationSvc,
		Concurrency: gatewayhttp.NewConcurrencyHelper(scheduler.NewConcurrencyService(concurrencyCache, scheduler.Diagnostics{Logf: logging.LegacyPrintf, Event: logging.Event}), gatewayhttp.SSEPingFormatNone, time.Second), Availability: newExecutionAvailabilityForTest(providerRepo, nil, cfg), Choices: gatewaySvcChoices,
	})

	handlerDone := make(chan struct{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(keyhttp.ContextKeyAPIKey), apiKey)
		c.Set(string(authctx.ContextKeyUser), authctx.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	router.GET("/openai/v1/responses", func(c *gin.Context) {
		h.ResponsesWebSocket(c)
		close(handlerDone)
	})
	handlerServer := httptest.NewServer(router)
	t.Cleanup(handlerServer.Close)

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(handlerServer.URL, "http")+"/openai/v1/responses", nil)
	cancelDial()
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientConn.CloseNow() })

	return &openAIWSPassthroughHandlerHarness{
		clientConn:     clientConn,
		handlerDone:    handlerDone,
		moderationRepo: moderationRepo,
		gatewayCache:   gatewayCache,
		apiKey:         apiKey,
		keys:           keys,
	}
}

func TestOpenAIResponsesWebSocketV2PassthroughCyberMarkIsConsumedAfterTurn(t *testing.T) {
	upstreamDone := make(chan struct{})
	secondUpstreamFrame := make(chan []byte, 1)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		require.NoError(t, err)
		defer func() { _ = conn.CloseNow() }()

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		_, _, err = conn.Read(readCtx)
		cancelRead()
		require.NoError(t, err)

		failed := []byte(`{"type":"response.failed","response":{"id":"resp_cyber_handler","model":"gpt-5.4","error":{"code":"cyber_policy","message":"blocked by upstream policy"},"usage":{"input_tokens":11,"output_tokens":3}}}`)
		writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
		err = conn.Write(writeCtx, coderws.MessageText, failed)
		cancelWrite()
		require.NoError(t, err)

		readCtx, cancelRead = context.WithTimeout(r.Context(), 3*time.Second)
		_, second, err := conn.Read(readCtx)
		cancelRead()
		if err != nil {
			return
		}
		secondUpstreamFrame <- append([]byte(nil), second...)

		completed := []byte(`{"type":"response.completed","response":{"id":"resp_cyber_handler_turn_2","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":1}}}`)
		writeCtx, cancelWrite = context.WithTimeout(r.Context(), 3*time.Second)
		err = conn.Write(writeCtx, coderws.MessageText, completed)
		cancelWrite()
		require.NoError(t, err)
	}))
	defer upstreamServer.Close()
	harness := newOpenAIWSPassthroughHandlerHarness(t, upstreamServer.URL)

	requestPayload := `{"type":"response.create","model":"gpt-5.4","prompt_cache_key":"cyber-session-1","input":"test"}`
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err := harness.clientConn.Write(writeCtx, coderws.MessageText, []byte(requestPayload))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, event, err := harness.clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "response.failed", gjson.GetBytes(event, "type").String())

	require.Eventually(t, func() bool {
		warnings := harness.moderationRepo.cyberWarningSnapshot()
		// 上游 warning 回调按网关错误记录为 502，WS 事件自身的格式没有 HTTP 状态字段。
		return len(warnings) == 1 && warnings[0].WarningText == "blocked by upstream policy" &&
			warnings[0].UpstreamStatus == http.StatusBadGateway
	}, 3*time.Second, 10*time.Millisecond, "handler AfterTurn must call recordCyberPolicyIfMarked and write the risk-control event")

	keyCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	keyCtx.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(requestPayload))
	blockKey := gatewayhttp.CyberSessionExplicitBlockKey(harness.apiKey.ID, keyCtx, []byte(requestPayload))
	require.NotEmpty(t, blockKey)
	store, ok := harness.gatewayCache.(session.CyberSessionBlockStore)
	require.True(t, ok)
	require.Eventually(t, func() bool {
		matched, findErr := store.FindCyberSessionBlocked(context.Background(), []string{blockKey})
		return findErr == nil && matched == blockKey
	}, 3*time.Second, 10*time.Millisecond, "handler AfterTurn must write the cyber session block table")

	writeCtx, cancelWrite = context.WithTimeout(context.Background(), 3*time.Second)
	err = harness.clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","prompt_cache_key":"cyber-session-1","input":"follow-up"}`))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead = context.WithTimeout(context.Background(), 3*time.Second)
	_, _, err = harness.clientConn.Read(readCtx)
	cancelRead()
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
	// 会话屏蔽使用固定英文关闭原因。
	require.Equal(t, "This session is blocked by the security policy. Start a new session.", closeErr.Reason)
	select {
	case <-harness.handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("websocket handler did not exit")
	}
	select {
	case <-upstreamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream websocket did not exit")
	}
	select {
	case second := <-secondUpstreamFrame:
		t.Fatalf("blocked follow-up reached upstream: %s", second)
	default:
	}
}

func TestOpenAIResponsesWebSocketV2PassthroughNonCyberTurnAllowsFollowup(t *testing.T) {
	upstreamDone := make(chan struct{})
	secondUpstreamFrame := make(chan []byte, 1)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(upstreamDone)
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		require.NoError(t, err)
		defer func() { _ = conn.CloseNow() }()

		readCtx, cancelRead := context.WithTimeout(r.Context(), 3*time.Second)
		_, _, err = conn.Read(readCtx)
		cancelRead()
		require.NoError(t, err)

		firstCompleted := []byte(`{"type":"response.completed","response":{"id":"resp_non_cyber_handler_turn_1","model":"gpt-5.4","usage":{"input_tokens":2,"output_tokens":1}}}`)
		writeCtx, cancelWrite := context.WithTimeout(r.Context(), 3*time.Second)
		err = conn.Write(writeCtx, coderws.MessageText, firstCompleted)
		cancelWrite()
		require.NoError(t, err)

		readCtx, cancelRead = context.WithTimeout(r.Context(), 3*time.Second)
		_, second, err := conn.Read(readCtx)
		cancelRead()
		require.NoError(t, err)
		secondUpstreamFrame <- append([]byte(nil), second...)

		secondCompleted := []byte(`{"type":"response.completed","response":{"id":"resp_non_cyber_handler_turn_2","model":"gpt-5.4","usage":{"input_tokens":3,"output_tokens":1}}}`)
		writeCtx, cancelWrite = context.WithTimeout(r.Context(), 3*time.Second)
		err = conn.Write(writeCtx, coderws.MessageText, secondCompleted)
		cancelWrite()
		require.NoError(t, err)

		readCtx, cancelRead = context.WithTimeout(r.Context(), 3*time.Second)
		_, _, _ = conn.Read(readCtx)
		cancelRead()
	}))
	defer upstreamServer.Close()
	harness := newOpenAIWSPassthroughHandlerHarness(t, upstreamServer.URL)

	firstPayload := `{"type":"response.create","model":"gpt-5.4","prompt_cache_key":"non-cyber-session-1","input":"first"}`
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err := harness.clientConn.Write(writeCtx, coderws.MessageText, []byte(firstPayload))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, firstEvent, err := harness.clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "resp_non_cyber_handler_turn_1", gjson.GetBytes(firstEvent, "response.id").String())

	secondPayload := `{"type":"response.create","model":"gpt-5.4","prompt_cache_key":"non-cyber-session-1","input":"follow-up"}`
	writeCtx, cancelWrite = context.WithTimeout(context.Background(), 3*time.Second)
	err = harness.clientConn.Write(writeCtx, coderws.MessageText, []byte(secondPayload))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead = context.WithTimeout(context.Background(), 3*time.Second)
	_, secondEvent, err := harness.clientConn.Read(readCtx)
	cancelRead()
	require.NoError(t, err)
	require.Equal(t, "resp_non_cyber_handler_turn_2", gjson.GetBytes(secondEvent, "response.id").String())
	require.Empty(t, harness.moderationRepo.cyberWarningSnapshot())

	keyCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	keyCtx.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(firstPayload))
	blockKey := gatewayhttp.CyberSessionExplicitBlockKey(harness.apiKey.ID, keyCtx, []byte(firstPayload))
	require.NotEmpty(t, blockKey)
	store, ok := harness.gatewayCache.(session.CyberSessionBlockStore)
	require.True(t, ok)
	matched, findErr := store.FindCyberSessionBlocked(context.Background(), []string{blockKey})
	require.NoError(t, findErr)
	require.Empty(t, matched)

	require.NoError(t, harness.clientConn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case <-harness.handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("non-cyber websocket handler did not exit")
	}
	select {
	case <-upstreamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("non-cyber upstream websocket did not exit")
	}
	select {
	case second := <-secondUpstreamFrame:
		require.JSONEq(t, secondPayload, string(second))
	default:
		t.Fatal("non-cyber follow-up did not reach upstream")
	}
}

// TestOpenAIResponsesWebSocketDeletedKeyRejectsFollowup 验证删除后新一轮不会到达上游。
func TestOpenAIResponsesWebSocketDeletedKeyRejectsFollowup(t *testing.T) {
	reached := make(chan bool, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			reached <- false
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if _, _, err = conn.Read(ctx); err != nil {
			reached <- false
			return
		}
		if err = conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_before_delete","model":"gpt-5.4","usage":{"input_tokens":2,"output_tokens":1}}}`)); err != nil {
			reached <- false
			return
		}
		_, _, err = conn.Read(ctx)
		reached <- err == nil
	}))
	defer upstream.Close()
	harness := newOpenAIWSPassthroughHandlerHarness(t, upstream.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	payload := []byte(`{"type":"response.create","model":"gpt-5.4","input":"test"}`)
	require.NoError(t, harness.clientConn.Write(ctx, coderws.MessageText, payload))
	_, event, err := harness.clientConn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "resp_before_delete", gjson.GetBytes(event, "response.id").String())
	harness.keys.remove()
	require.NoError(t, harness.clientConn.Write(ctx, coderws.MessageText, payload))
	_, _, err = harness.clientConn.Read(ctx)
	require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(err))
	select {
	case forwarded := <-reached:
		require.False(t, forwarded, "删除后的第二轮不能到达上游")
	case <-ctx.Done():
		t.Fatal("上游连接未结束")
	}
	select {
	case <-harness.handlerDone:
	case <-ctx.Done():
		t.Fatal("删除后的连接未结束")
	}
}
