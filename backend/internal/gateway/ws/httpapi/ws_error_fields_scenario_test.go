package httpapi

// WS 错误字段场景覆盖 protocol/openai/ws_events.go、upstream/openai/ws_error_rules.go 和 gateway/provider/ws_diagnostics.go，测量字段解析后复用分类与诊断结果的开销。

import (
	"testing"

	gatewayprovider "github.com/TokenFlux/TokenRouter/internal/gateway/provider"
	protocolopenai "github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/upstream/openai"
)

var (
	benchmarkOpenAIWSStringSink string
	benchmarkOpenAIWSBoolSink   bool
)

func BenchmarkOpenAIWSErrorEventFieldReuse(b *testing.B) {
	event := []byte(`{"type":"error","error":{"type":"invalid_request_error","code":"invalid_request","message":"invalid input"}}`)
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		codeRaw, errTypeRaw, errMsgRaw := protocolopenai.ParseWSErrorEventFields(event)
		benchmarkOpenAIWSStringSink, benchmarkOpenAIWSBoolSink = openai.ClassifyWSErrorEventFromRaw(codeRaw, errTypeRaw, errMsgRaw)
		code, errType, errMsg := gatewayprovider.SummarizeOpenAIWSErrorEventFieldsFromRaw(codeRaw, errTypeRaw, errMsgRaw)
		benchmarkOpenAIWSStringSink = code
		benchmarkOpenAIWSStringSink = errType
		benchmarkOpenAIWSStringSink = errMsg
		benchmarkOpenAIWSBoolSink = openai.WSErrorHTTPStatusFromRaw(codeRaw, errTypeRaw) > 0
	}
}
