package openai

import (
	"encoding/json"

	"github.com/tidwall/gjson"
)

// IsWSWarmupPayload 识别客户端明确声明的无生成预热请求。
func IsWSWarmupPayload(payload []byte) bool {
	return gjson.ValidBytes(payload) && gjson.GetBytes(payload, "type").String() == "response.create" && gjson.GetBytes(payload, "generate").Type == gjson.False
}

// WSWarmupEvents 构造带稳定响应 ID 的空预热事件，时间与 ID 由调用方提供。
// @project-doc docs/interfaces/openai_upstream.md#responses_ws_warmup
func WSWarmupEvents(responseID, model string, createdAt int64) ([][]byte, error) {
	response := ResponsesResponse{ID: responseID, Object: "response", CreatedAt: createdAt, Model: model, Output: []ResponsesOutput{}}
	events := make([][]byte, 0, 2)
	for sequence, status := range []string{"in_progress", "completed"} {
		response.Status = status
		eventType := "response.created"
		if status == "completed" {
			eventType = "response.completed"
			response.Usage = &ResponsesUsage{}
		}
		event := struct {
			Type           string            `json:"type"`
			SequenceNumber int               `json:"sequence_number"`
			Response       ResponsesResponse `json:"response"`
		}{eventType, sequence, response}
		body, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		events = append(events, body)
	}
	return events, nil
}
