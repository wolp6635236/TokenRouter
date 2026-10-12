package ops

import (
	"encoding/json"
	"strings"
)

func ParseOpsUpstreamErrors(raw string) ([]*OpsUpstreamErrorEvent, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []*OpsUpstreamErrorEvent{}, nil
	}
	// 读取历史日志时归一化实体字段，同时存在新旧字段时优先使用新字段。
	var records []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &records); err != nil {
		return nil, err
	}
	for _, record := range records {
		for old, current := range map[string]string{"account_id": "provider_id", "account_name": "provider_name"} {
			if value, exists := record[old]; exists {
				if _, present := record[current]; !present {
					record[current] = value
				}
				delete(record, old)
			}
		}
	}
	normalized, err := json.Marshal(records)
	if err != nil {
		return nil, err
	}
	var out []*OpsUpstreamErrorEvent
	if err := json.Unmarshal(normalized, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func marshalOpsUpstreamErrors(events []*OpsUpstreamErrorEvent) *string {
	if len(events) == 0 {
		return nil
	}
	// Ensure we always store a valid JSON value.
	raw, err := json.Marshal(events)
	if err != nil || len(raw) == 0 {
		return nil
	}
	s := string(raw)
	return &s
}
