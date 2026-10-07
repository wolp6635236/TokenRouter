package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TokenFlux/TokenRouter/internal/gateway/forward"
	"github.com/TokenFlux/TokenRouter/internal/gateway/httpapi"
	"github.com/TokenFlux/TokenRouter/internal/gateway/provider/messageforward"
	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// TestMessagesConversionSSEFormats 将同一批文本和工具事件以不同 SSE 排版送入 HTTP 转换器。
func TestMessagesConversionSSEFormats(t *testing.T) {
	textStream := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"upstream-model\",\"content\":[],\"usage\":{\"input_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"PONG\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	for _, format := range []string{"standard", "data_only", "comments", "multiline", "event_only_type", "eof"} {
		for _, content := range []string{"text", "tool"} {
			for _, mode := range []string{"responses_stream", "responses_buffered", "chat_stream", "chat_buffered"} {
				t.Run(format+"/"+content+"/"+mode, func(t *testing.T) {
					body := textStream
					if content == "tool" {
						body = strings.NewReplacer("lookup", "codex_app__read_thread", "toolu_1", "toolu_namespace", "query", "thread_id").Replace(toolAnthropicSSEStream())
					}
					body = conversionSSEFormat(t, body, format)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
					input := conversionResponseFixture(resp)
					output := httpapi.NewMessageForwardBoundary(c, nil).ConversionOutput(strings.HasPrefix(mode, "responses"), &messageforward.AttemptState{})
					var result *forward.Result
					var err error
					switch mode {
					case "responses_stream":
						result, err = forward.ResponsesStreaming(input, output, "client-model", "upstream-model", nil, time.Now(), namespaceToolMapping())
					case "responses_buffered":
						result, err = forward.ResponsesBuffered(input, output, "client-model", "upstream-model", nil, time.Now(), namespaceToolMapping())
					case "chat_stream":
						result, err = forward.ChatStreaming(input, output, "client-model", "upstream-model", nil, time.Now(), true)
					case "chat_buffered":
						result, err = forward.ChatBuffered(input, output, "client-model", "upstream-model", nil, time.Now())
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, 10, result.Usage.InputTokens)
					require.Equal(t, 5, result.Usage.OutputTokens)
					if content == "text" {
						require.Contains(t, rec.Body.String(), "PONG")
					} else {
						require.Contains(t, rec.Body.String(), "read_thread")
						require.Contains(t, rec.Body.String(), "toolu_namespace")
						require.Contains(t, rec.Body.String(), "thread_id")
					}
					if mode == "responses_stream" {
						completed := 0
						toolCalls := 0
						openai.ForEachOpenAISSEFrame(rec.Body.String(), func(eventType string, data []byte) {
							require.Equal(t, eventType, gjson.GetBytes(data, "type").String())
							if eventType == "response.completed" {
								completed++
								require.EqualValues(t, 5, gjson.GetBytes(data, "response.usage.output_tokens").Int())
							}
							if eventType == "response.output_item.done" && gjson.GetBytes(data, "item.type").String() == "function_call" {
								toolCalls++
								require.Equal(t, "codex_app", gjson.GetBytes(data, "item.namespace").String())
								require.JSONEq(t, `{"thread_id":"status"}`, gjson.GetBytes(data, "item.arguments").String())
							}
						})
						require.Equal(t, 1, completed)
						if content == "tool" {
							require.Equal(t, 1, toolCalls)
						}
					}
				})
			}
		}
	}
}

// conversionSSEFormat 保持事件内容相同，改变传输排版以覆盖上游的 data-only 输出。
func conversionSSEFormat(t *testing.T, body, format string) string {
	t.Helper()
	var result strings.Builder
	openai.ForEachOpenAISSEFrame(body, func(eventType string, data []byte) {
		if format == "event_only_type" {
			var err error
			data, err = sjson.DeleteBytes(data, "type")
			require.NoError(t, err)
		}
		if format != "data_only" {
			_, _ = result.WriteString("event:" + eventType + "\n")
		}
		if format == "comments" {
			_, _ = result.WriteString(": heartbeat\nid: test\n")
		}
		payload := string(data)
		if format == "multiline" {
			payload = "{\ndata:" + strings.TrimPrefix(payload, "{")
		}
		_, _ = result.WriteString("data:" + payload + "\n\n")
	})
	if format == "eof" {
		return strings.TrimRight(result.String(), "\n")
	}
	return result.String()
}

type conversionReadFailure struct{}

func (conversionReadFailure) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// TestMessagesConversionSSEFailures 检查尾部用量交付、截断 JSON 和累计帧超限的 HTTP 输出。
func TestMessagesConversionSSEFailures(t *testing.T) {
	const start = "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_failure\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"usage\":{\"input_tokens\":10}}}\n\n"
	const usage = "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"},\"usage\":{\"output_tokens\":25}}\n"
	for _, mode := range []string{"responses_stream", "responses_buffered", "chat_stream", "chat_buffered"} {
		for _, failure := range []string{"complete_json", "completed", "completed_with_stop", "truncated_json", "frame_limit"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				body := start + usage
				if failure == "completed" || failure == "completed_with_stop" {
					body = strings.ReplaceAll(body, "max_tokens", "end_turn")
				}
				if failure == "completed_with_stop" {
					body += "\ndata: {\"type\":\"message_stop\"}\n\n"
				}
				if failure == "truncated_json" {
					body = start + `data: {"type":"message_delta","usage":{"output_tokens":`
				}
				if failure == "frame_limit" {
					body = start + strings.Repeat("data: "+strings.Repeat("x", 60)+"\n", 20)
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				path := "/v1/responses"
				if strings.HasPrefix(mode, "chat") {
					path = "/v1/chat/completions"
				}
				c.Request = httptest.NewRequest(http.MethodPost, path, nil)
				before := c.Writer.Size()
				reader := io.MultiReader(strings.NewReader(body), conversionReadFailure{})
				resp := &http.Response{Body: io.NopCloser(reader)}
				input := conversionResponseFixture(resp)
				input.MaxSSEFrameBytes = 512
				output := httpapi.NewMessageForwardBoundary(c, nil).ConversionOutput(strings.HasPrefix(mode, "responses"), &messageforward.AttemptState{})
				var result *forward.Result
				var err error
				switch mode {
				case "responses_stream":
					result, err = forward.ResponsesStreaming(input, output, "m", "m", nil, time.Now(), namespaceToolMapping())
				case "responses_buffered":
					result, err = forward.ResponsesBuffered(input, output, "m", "m", nil, time.Now(), namespaceToolMapping())
				case "chat_stream":
					result, err = forward.ChatStreaming(input, output, "m", "m", nil, time.Now(), true)
				case "chat_buffered":
					result, err = forward.ChatBuffered(input, output, "m", "m", nil, time.Now())
				}
				if failure == "frame_limit" {
					require.ErrorIs(t, err, forward.ErrConversionSSEFrameTooLarge)
				} else {
					require.ErrorIs(t, err, io.ErrUnexpectedEOF)
				}
				require.NotNil(t, result)
				require.Equal(t, 10, result.Usage.InputTokens)
				if failure == "complete_json" || failure == "completed" || failure == "completed_with_stop" {
					require.Equal(t, 25, result.Usage.OutputTokens)
					if strings.HasPrefix(mode, "responses") {
						status := "incomplete"
						if failure != "complete_json" {
							status = "completed"
						}
						if mode == "responses_buffered" {
							require.Equal(t, status, gjson.GetBytes(rec.Body.Bytes(), "status").String())
						} else {
							terminals := 0
							openai.ForEachOpenAISSEFrame(rec.Body.String(), func(kind string, data []byte) {
								if kind == "response.completed" || kind == "response.incomplete" {
									terminals++
									require.Equal(t, status, gjson.GetBytes(data, "response.status").String())
								}
							})
							require.Equal(t, 1, terminals)
						}
					} else {
						reason := "length"
						if failure != "complete_json" {
							reason = "stop"
						}
						require.Contains(t, rec.Body.String(), `"finish_reason":"`+reason+`"`)
					}
				} else {
					require.Zero(t, result.Usage.OutputTokens)
					require.Contains(t, rec.Body.String(), "upstream_stream_error")
					require.NotContains(t, rec.Body.String(), "response.completed")
					require.NotContains(t, rec.Body.String(), "[DONE]")
				}
				// 模拟外层 OtherFailure 的补发判定，已经交付的终态和 JSON 应保持原样。
				responseBody := rec.Body.String()
				if !httpapi.OpenAIForwardErrorAlreadyCommunicated(c, before, err) {
					require.False(t, httpapi.DefaultOpenAIErrorOutput().EnsureResponse(c, false, err))
				}
				require.Equal(t, responseBody, rec.Body.String())
			})
		}
	}
}

type conversionFailingWriter struct{ gin.ResponseWriter }

func (conversionFailingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// TestConversionTerminalCommit 检查部分输出和写失败时仍允许外层处理错误。
func TestConversionTerminalCommit(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		if failWrite {
			c.Writer = conversionFailingWriter{ResponseWriter: c.Writer}
		}
		output := httpapi.NewMessageForwardBoundary(c, nil).ConversionOutput(true, &messageforward.AttemptState{})
		_, _ = output.Event("response.output_text.delta", []byte(`{"type":"response.output_text.delta","delta":"hi"}`))
		require.False(t, httpapi.IsResponseCommitted(c))
		_, err := output.Event("response.failed", []byte(`{"type":"response.failed","response":{"status":"failed"}}`))
		if failWrite {
			require.ErrorIs(t, err, io.ErrClosedPipe)
			require.False(t, httpapi.IsResponseCommitted(c))
		} else {
			require.NoError(t, err)
			require.True(t, httpapi.IsResponseCommitted(c))
		}
	}
}
