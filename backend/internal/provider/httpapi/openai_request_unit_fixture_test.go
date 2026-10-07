package httpapi

import (
	"testing"

	"github.com/TokenFlux/TokenRouter/internal/provider"
	provideradapter "github.com/TokenFlux/TokenRouter/internal/provider/provider"
)

func executeOpenAIProbeRequestType(t *testing.T, executor *provideradapter.OpenAIProviderTest, output *openAIProbeOutput, id int64, model, prompt, kind, mode, protocol string) error {
	t.Helper()
	return openAIProbeCore(executor).Test(output.Request.Context(), provider.TestRequest{ProviderID: id, Model: model, Prompt: prompt, Mode: mode, Type: &kind, Protocol: protocol}, NewTestEventSink(output.recorder))
}
