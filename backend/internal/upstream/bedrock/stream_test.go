package bedrock

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExtractBedrockChunkData(t *testing.T) {
	t.Run("valid base64 payload", func(t *testing.T) {
		original := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`
		b64 := base64.StdEncoding.EncodeToString([]byte(original))
		payload := []byte(`{"bytes":"` + b64 + `"}`)

		result := ExtractBedrockChunkData(payload)
		require.NotNil(t, result)
		assert.JSONEq(t, original, string(result))
	})

	t.Run("empty bytes field", func(t *testing.T) {
		result := ExtractBedrockChunkData([]byte(`{"bytes":""}`))
		assert.Nil(t, result)
	})

	t.Run("no bytes field", func(t *testing.T) {
		result := ExtractBedrockChunkData([]byte(`{"other":"value"}`))
		assert.Nil(t, result)
	})

	t.Run("invalid base64", func(t *testing.T) {
		result := ExtractBedrockChunkData([]byte(`{"bytes":"not-valid-base64!!!"}`))
		assert.Nil(t, result)
	})
}

func TestTransformBedrockInvocationMetrics(t *testing.T) {
	t.Run("converts metrics to usage", func(t *testing.T) {
		input := `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"amazon-bedrock-invocationMetrics":{"inputTokenCount":150,"outputTokenCount":42}}`
		result := TransformBedrockInvocationMetrics([]byte(input))

		// 用量转换后删除 amazon-bedrock-invocationMetrics。
		assert.False(t, gjson.GetBytes(result, "amazon-bedrock-invocationMetrics").Exists())
		// 用量写入 usage。
		assert.Equal(t, int64(150), gjson.GetBytes(result, "usage.input_tokens").Int())
		assert.Equal(t, int64(42), gjson.GetBytes(result, "usage.output_tokens").Int())
		// 其他字段保持不变。
		assert.Equal(t, "message_delta", gjson.GetBytes(result, "type").String())
		assert.Equal(t, "end_turn", gjson.GetBytes(result, "delta.stop_reason").String())
	})

	t.Run("no metrics present", func(t *testing.T) {
		input := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`
		result := TransformBedrockInvocationMetrics([]byte(input))
		assert.JSONEq(t, input, string(result))
	})

	t.Run("does not overwrite existing usage", func(t *testing.T) {
		input := `{"type":"message_delta","usage":{"output_tokens":100},"amazon-bedrock-invocationMetrics":{"inputTokenCount":150,"outputTokenCount":42}}`
		result := TransformBedrockInvocationMetrics([]byte(input))

		// 已有 usage 时直接删除 metrics。
		assert.False(t, gjson.GetBytes(result, "amazon-bedrock-invocationMetrics").Exists())
		assert.Equal(t, int64(100), gjson.GetBytes(result, "usage.output_tokens").Int())
	})
}

func TestExtractEventStreamHeaderValue(t *testing.T) {
	// 构造 :event-type 为 "chunk" 的字符串头（类型值为 7）。
	buildStringHeader := func(name, value string) []byte {
		var buf bytes.Buffer
		// 名称长度占 1 字节。
		_ = buf.WriteByte(byte(len(name)))
		// 名称正文。
		_, _ = buf.WriteString(name)
		// 类型值 7 表示字符串。
		_ = buf.WriteByte(7)
		// 值长度占 2 字节，使用大端序。
		_ = binary.Write(&buf, binary.BigEndian, uint16(len(value)))
		// 值正文。
		_, _ = buf.WriteString(value)
		return buf.Bytes()
	}

	t.Run("find string header", func(t *testing.T) {
		headers := buildStringHeader(":event-type", "chunk")
		assert.Equal(t, "chunk", ExtractEventStreamHeaderValue(headers, ":event-type"))
	})

	t.Run("header not found", func(t *testing.T) {
		headers := buildStringHeader(":event-type", "chunk")
		assert.Equal(t, "", ExtractEventStreamHeaderValue(headers, ":message-type"))
	})

	t.Run("multiple headers", func(t *testing.T) {
		var buf bytes.Buffer
		_, _ = buf.Write(buildStringHeader(":content-type", "application/json"))
		_, _ = buf.Write(buildStringHeader(":event-type", "chunk"))
		_, _ = buf.Write(buildStringHeader(":message-type", "event"))

		headers := buf.Bytes()
		assert.Equal(t, "chunk", ExtractEventStreamHeaderValue(headers, ":event-type"))
		assert.Equal(t, "application/json", ExtractEventStreamHeaderValue(headers, ":content-type"))
		assert.Equal(t, "event", ExtractEventStreamHeaderValue(headers, ":message-type"))
	})

	t.Run("empty headers", func(t *testing.T) {
		assert.Equal(t, "", ExtractEventStreamHeaderValue([]byte{}, ":event-type"))
	})
}

func TestBedrockEventStreamDecoder(t *testing.T) {
	crc32IeeeTab := crc32.MakeTable(crc32.IEEE)

	// EventStream 帧使用 CRC32/IEEE 校验和。
	buildFrame := func(eventType string, payload []byte) []byte {
		// 构造帧头。
		var headersBuf bytes.Buffer
		// 事件类型头。
		_ = headersBuf.WriteByte(byte(len(":event-type")))
		_, _ = headersBuf.WriteString(":event-type")
		_ = headersBuf.WriteByte(7) // 字符串类型。
		_ = binary.Write(&headersBuf, binary.BigEndian, uint16(len(eventType)))
		_, _ = headersBuf.WriteString(eventType)
		// 消息类型头。
		_ = headersBuf.WriteByte(byte(len(":message-type")))
		_, _ = headersBuf.WriteString(":message-type")
		_ = headersBuf.WriteByte(7)
		_ = binary.Write(&headersBuf, binary.BigEndian, uint16(len("event")))
		_, _ = headersBuf.WriteString("event")

		headers := headersBuf.Bytes()
		headersLen := uint32(len(headers))
		// 总长度包含 12 字节前导、帧头、报文和 4 字节消息校验和。
		totalLen := uint32(12 + len(headers) + len(payload) + 4)

		// 前导由 4 字节总长度和 4 字节帧头长度组成。
		var preludeBuf bytes.Buffer
		_ = binary.Write(&preludeBuf, binary.BigEndian, totalLen)
		_ = binary.Write(&preludeBuf, binary.BigEndian, headersLen)
		preludeBytes := preludeBuf.Bytes()
		preludeCRC := crc32.Checksum(preludeBytes, crc32IeeeTab)

		// 帧按前导、前导校验和、帧头和报文的顺序拼接。
		var frame bytes.Buffer
		_, _ = frame.Write(preludeBytes)
		_ = binary.Write(&frame, binary.BigEndian, preludeCRC)
		_, _ = frame.Write(headers)
		_, _ = frame.Write(payload)

		// 消息校验和覆盖它之前的全部字节。
		messageCRC := crc32.Checksum(frame.Bytes(), crc32IeeeTab)
		_ = binary.Write(&frame, binary.BigEndian, messageCRC)
		return frame.Bytes()
	}

	t.Run("decode chunk event", func(t *testing.T) {
		payload := []byte(`{"bytes":"dGVzdA=="}`) // base64("test")
		frame := buildFrame("chunk", payload)

		decoder := NewBedrockEventStreamDecoder(bytes.NewReader(frame))
		result, err := decoder.Decode()
		require.NoError(t, err)
		assert.Equal(t, payload, result)
	})

	t.Run("skip non-chunk events", func(t *testing.T) {
		// initial-response 后写入 chunk 事件。
		var buf bytes.Buffer
		_, _ = buf.Write(buildFrame("initial-response", []byte(`{}`)))
		chunkPayload := []byte(`{"bytes":"aGVsbG8="}`)
		_, _ = buf.Write(buildFrame("chunk", chunkPayload))

		decoder := NewBedrockEventStreamDecoder(&buf)
		result, err := decoder.Decode()
		require.NoError(t, err)
		assert.Equal(t, chunkPayload, result)
	})

	t.Run("EOF on empty input", func(t *testing.T) {
		decoder := NewBedrockEventStreamDecoder(bytes.NewReader(nil))
		_, err := decoder.Decode()
		assert.Equal(t, io.EOF, err)
	})

	t.Run("corrupted prelude CRC", func(t *testing.T) {
		frame := buildFrame("chunk", []byte(`{"bytes":"dGVzdA=="}`))
		// 破坏第 8 至 11 字节的前导校验和。
		frame[8] ^= 0xFF
		decoder := NewBedrockEventStreamDecoder(bytes.NewReader(frame))
		_, err := decoder.Decode()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prelude CRC mismatch")
	})

	t.Run("corrupted message CRC", func(t *testing.T) {
		frame := buildFrame("chunk", []byte(`{"bytes":"dGVzdA=="}`))
		// 破坏末尾 4 字节的消息校验和。
		frame[len(frame)-1] ^= 0xFF
		decoder := NewBedrockEventStreamDecoder(bytes.NewReader(frame))
		_, err := decoder.Decode()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "message CRC mismatch")
	})

	t.Run("castagnoli encoded frame is rejected", func(t *testing.T) {
		castagnoliTab := crc32.MakeTable(crc32.Castagnoli)
		payload := []byte(`{"bytes":"dGVzdA=="}`)

		var headersBuf bytes.Buffer
		_ = headersBuf.WriteByte(byte(len(":event-type")))
		_, _ = headersBuf.WriteString(":event-type")
		_ = headersBuf.WriteByte(7)
		_ = binary.Write(&headersBuf, binary.BigEndian, uint16(len("chunk")))
		_, _ = headersBuf.WriteString("chunk")

		headers := headersBuf.Bytes()
		headersLen := uint32(len(headers))
		totalLen := uint32(12 + len(headers) + len(payload) + 4)

		var preludeBuf bytes.Buffer
		_ = binary.Write(&preludeBuf, binary.BigEndian, totalLen)
		_ = binary.Write(&preludeBuf, binary.BigEndian, headersLen)
		preludeBytes := preludeBuf.Bytes()

		var frame bytes.Buffer
		_, _ = frame.Write(preludeBytes)
		_ = binary.Write(&frame, binary.BigEndian, crc32.Checksum(preludeBytes, castagnoliTab))
		_, _ = frame.Write(headers)
		_, _ = frame.Write(payload)
		_ = binary.Write(&frame, binary.BigEndian, crc32.Checksum(frame.Bytes(), castagnoliTab))

		decoder := NewBedrockEventStreamDecoder(bytes.NewReader(frame.Bytes()))
		_, err := decoder.Decode()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "prelude CRC mismatch")
	})
}
