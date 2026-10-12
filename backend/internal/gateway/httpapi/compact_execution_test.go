package httpapi

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	providercore "github.com/TokenFlux/TokenRouter/internal/provider"
)

func TestPrepareOpenAICompactFallbackRetryRequiresExplicitCompact(t *testing.T) {
	svc := newResponsesFixture(responsesFixtureInputs{compactModel: "gpt-5.4"})
	c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":"hello"}]}`)
	errorBody := []byte(`{"error":{"code":"context_length_exceeded","message":"maximum context length exceeded"}}`)

	retryBody, fallbackModel, retry := svc.Text.Compact.Prepare(
		c, nil, "gpt-5.5", body, http.StatusBadRequest, "maximum context length exceeded", errorBody, false,
	)

	require.False(t, retry)
	require.Empty(t, fallbackModel)
	require.Equal(t, body, retryBody)
}

func TestPrepareOpenAICompactFallbackRetryPreservesNativeTriggerAndContext(t *testing.T) {
	svc := newResponsesFixture(responsesFixtureInputs{compactModel: "gpt-5.4"})
	c := newOpenAICompactFallbackTestContext(t, "/v1/responses")
	MarkOpenAINativeCompactionV2(c)
	body := []byte(`{"model":"gpt-5.5","stream":true,"input":[{"type":"message","role":"user","content":"hello"},{"type":"compaction_trigger"}]}`)
	errorBody := []byte(`{"error":{"code":"context_length_exceeded","message":"context window exceeded"}}`)
	pathBefore := OpenAIResponsesRequestPathSuffix(c)

	retryBody, fallbackModel, retry := svc.Text.Compact.Prepare(
		c, nil, "gpt-5.5", body, http.StatusBadRequest, "context window exceeded", errorBody, false,
	)

	require.True(t, retry)
	require.Equal(t, "gpt-5.4", fallbackModel)
	require.Equal(t, "gpt-5.4", gjson.GetBytes(retryBody, "model").String())
	require.True(t, protocolopenai.HasCompactionTriggerInInput(retryBody))
	require.True(t, IsOpenAINativeCompactionV2(c))
	require.Equal(t, pathBefore, OpenAIResponsesRequestPathSuffix(c))
}

func TestResolveOpenAICompactFallbackModelPrefersProviderMapping(t *testing.T) {
	svc := newResponsesFixture(responsesFixtureInputs{compactModel: "global-compact"})
	provider := &gatewayprovider.ExecutionProvider{Record: providercore.Record{LoadLocation: time.LoadLocation, Credentials: map[string]any{
		"compact_model_mapping": map[string]any{"gpt-5.5": "provider-compact"},
	}}}

	require.Equal(t, "provider-compact", svc.Text.Compact.ResolveModel(provider, "gpt-5.5"))
	require.Equal(t, "global-compact", svc.Text.Compact.ResolveModel(provider, "unmapped-model"))
}

func TestPrepareOpenAICompactFallbackRetryLegacyPathAndSingleAttemptGuard(t *testing.T) {
	svc := newResponsesFixture(responsesFixtureInputs{compactModel: "gpt-5.4"})
	c := newOpenAICompactFallbackTestContext(t, "/v1/responses/compact")
	body := []byte(`{"model":"gpt-5.5","input":[]}`)
	errorBody := []byte(`{"response":{"status":"failed","error":null}}`)

	retryBody, fallbackModel, retry := svc.Text.Compact.Prepare(
		c, nil, "gpt-5.5", body, http.StatusBadRequest, "", errorBody, false,
	)
	require.True(t, retry)
	require.Equal(t, "gpt-5.4", fallbackModel)
	require.Equal(t, "/compact", OpenAIResponsesRequestPathSuffix(c))

	secondBody, secondModel, secondRetry := svc.Text.Compact.Prepare(
		c, nil, "gpt-5.5", retryBody, http.StatusBadRequest, "", errorBody, true,
	)
	require.False(t, secondRetry)
	require.Empty(t, secondModel)
	require.Equal(t, retryBody, secondBody)
}

func TestPrepareOpenAICompactFallbackRetryDoesNotHideSpecificBusinessFailure(t *testing.T) {
	svc := newResponsesFixture(responsesFixtureInputs{compactModel: "gpt-5.4"})
	c := newOpenAICompactFallbackTestContext(t, "/v1/responses/compact")
	body := []byte(`{"model":"gpt-5.5","input":[]}`)
	errorBody := []byte(`{"response":{"status":"failed","error":{"type":"permission_error","message":"workspace denied"}}}`)

	retryBody, fallbackModel, retry := svc.Text.Compact.Prepare(
		c, nil, "gpt-5.5", body, http.StatusBadRequest, "workspace denied", errorBody, false,
	)

	require.False(t, retry)
	require.Empty(t, fallbackModel)
	require.Equal(t, body, retryBody)
}

func TestIsOpenAICompactModelFailureRequiresExplicitModelAvailabilityMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    bool
	}{
		{name: "explicit unsupported model", message: "The requested model is not supported", want: true},
		{name: "named missing model", message: "The model `gpt-5.5` does not exist", want: true},
		{name: "unsupported model code-like message", message: "unsupported model: gpt-5.5", want: true},
		{name: "unsupported model feature", message: "This model output format is not supported", want: false},
		{name: "unsupported parameter for model", message: "Parameter tools is not supported for this model", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, (gatewayprovider.CompactModels{}).Recovery(nil).ModelFailure(
				http.StatusBadRequest,
				tt.message,
				[]byte(`{"error":{"message":`+strconv.Quote(tt.message)+`}}`),
			))
		})
	}
}

func TestPrepareOpenAICompactFallbackRetrySkipsSameModel(t *testing.T) {
	svc := newResponsesFixture(responsesFixtureInputs{compactModel: "gpt-5.5"})
	c := newOpenAICompactFallbackTestContext(t, "/v1/responses/compact")
	body := []byte(`{"model":"gpt-5.5","input":[]}`)
	errorBody := []byte(`{"error":{"code":"model_not_found","message":"model not found"}}`)

	_, _, retry := svc.Text.Compact.Prepare(
		c, nil, "gpt-5.5", body, http.StatusNotFound, "model not found", errorBody, false,
	)
	require.False(t, retry)
}
