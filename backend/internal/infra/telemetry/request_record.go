package telemetry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const requestCaptureKey ContextKey = "request_capture"

// RequestAlias 保存外部标识、计费键和关联请求。
type RequestAlias struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// RequestAttempt 保存一次上游尝试的诊断元数据。
type RequestAttempt struct {
	Number     int    `json:"number"`
	ProviderID int64  `json:"provider_id,omitempty"`
	RequestID  string `json:"upstream_request_id,omitempty"`
	Status     int    `json:"status_code,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
}

// RequestRecord 是独立于监控采样的请求摘要，正文和凭据留在业务处理器中。
type RequestRecord struct {
	ErrorCode       string           `json:"error_code,omitempty"`
	RequestID       string           `json:"request_id"`
	ParentRequestID string           `json:"parent_request_id,omitempty"`
	StartedAt       time.Time        `json:"started_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	FinishedAt      *time.Time       `json:"finished_at,omitempty"`
	Method          string           `json:"method,omitempty"`
	Path            string           `json:"path,omitempty"`
	State           string           `json:"state"`
	Status          int              `json:"status_code,omitempty"`
	DurationMs      int64            `json:"duration_ms,omitempty"`
	UserID          int64            `json:"user_id,omitempty"`
	TeamID          int64            `json:"team_id,omitempty"`
	APIKeyID        int64            `json:"api_key_id,omitempty"`
	ProviderID      int64            `json:"provider_id,omitempty"`
	Platform        string           `json:"platform,omitempty"`
	Model           string           `json:"model,omitempty"`
	Aliases         []RequestAlias   `json:"aliases,omitempty"`
	Attempts        []RequestAttempt `json:"attempts,omitempty"`
	Timings         map[string]int64 `json:"timings,omitempty"`
}

// RequestCapture 串行化同一次请求的元数据更新和持久化通知。
type RequestCapture struct {
	mu      sync.Mutex
	record  RequestRecord
	observe func(RequestRecord)
}

// NewRequestID 为独立调用生成 UUID。
func NewRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return formatRequestID(b[:])
}

// StableRequestID 为后台任务派生可重放的请求 ID。
func StableRequestID(key string) string {
	b := sha256.Sum256([]byte("TokenRouter/request/" + key))
	b[6] = b[6]&0x0f | 0x80
	b[8] = b[8]&0x3f | 0x80
	return formatRequestID(b[:16])
}

func formatRequestID(b []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// RequestIDValue 返回请求入口分配的 ID。
func RequestIDValue(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(RequestID).(string)
	return strings.TrimSpace(id)
}

// WithRequestCapture 在上下文中登记请求记录及其观察者。
func WithRequestCapture(ctx context.Context, record RequestRecord, observe func(RequestRecord)) context.Context {
	capture := &RequestCapture{record: record, observe: observe}
	ctx = context.WithValue(ctx, requestCaptureKey, capture)
	UpdateRequest(ctx, func(*RequestRecord) {})
	return ctx
}

// UpdateRequest 在同步更新后交付完整快照，后台接收方可以按更新时间去重。
func UpdateRequest(ctx context.Context, update func(*RequestRecord)) {
	if ctx == nil {
		return
	}
	capture, _ := ctx.Value(requestCaptureKey).(*RequestCapture)
	if capture == nil {
		return
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	previous := cloneRequestRecord(capture.record)
	update(&capture.record)
	if !previous.UpdatedAt.IsZero() && reflect.DeepEqual(previous, capture.record) {
		return
	}
	now := time.Now().UTC()
	if !now.After(capture.record.UpdatedAt) {
		now = capture.record.UpdatedAt.Add(time.Nanosecond)
	}
	capture.record.UpdatedAt = now
	if capture.observe != nil {
		capture.observe(cloneRequestRecord(capture.record))
	}
}

// UpdateRequestForID 更新指定请求的记录器，返回是否找到对应请求。
func UpdateRequestForID(ctx context.Context, id string, update func(*RequestRecord)) bool {
	matched := false
	UpdateRequest(ctx, func(record *RequestRecord) {
		if record.RequestID == id {
			matched = true
			update(record)
		}
	})
	return matched
}

// AddRequestAlias 把外部或历史标识登记为可检索的别名。
func AddRequestAlias(ctx context.Context, kind, value string) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || strings.ContainsAny(value, "\r\n\x00") {
		return
	}
	UpdateRequest(ctx, func(record *RequestRecord) {
		alias := RequestAlias{Kind: kind, Value: value}
		if slices.Contains(record.Aliases, alias) {
			return
		}
		if len(record.Aliases) < 128 {
			record.Aliases = append(record.Aliases, alias)
		}
	})
}

// CopyRequestCapture 把同一请求的记录器传给脱离 HTTP 生命周期的完成任务。
func CopyRequestCapture(destination, source context.Context) context.Context {
	if source == nil {
		return destination
	}
	if capture, ok := source.Value(requestCaptureKey).(*RequestCapture); ok {
		return context.WithValue(destination, requestCaptureKey, capture)
	}
	return destination
}

// RecordRelatedRequest 保存连接轮次或后台任务，并登记到发起请求。
func RecordRelatedRequest(ctx context.Context, record RequestRecord) {
	if ctx == nil {
		return
	}
	capture, _ := ctx.Value(requestCaptureKey).(*RequestCapture)
	if capture == nil {
		return
	}
	capture.mu.Lock()
	parent := cloneRequestRecord(capture.record)
	observe := capture.observe
	capture.mu.Unlock()
	record.ParentRequestID = parent.RequestID
	if record.UserID == 0 {
		record.UserID = parent.UserID
	}
	if record.TeamID == 0 {
		record.TeamID = parent.TeamID
	}
	if record.APIKeyID == 0 {
		record.APIKeyID = parent.APIKeyID
	}
	if record.Method == "" {
		record.Method = parent.Method
	}
	if record.Path == "" {
		record.Path = parent.Path
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = time.Now().UTC()
	}
	record.UpdatedAt = time.Now().UTC()
	if record.FinishedAt == nil && (record.State == "completed" || record.State == "failed" || record.State == "canceled") {
		finished := record.UpdatedAt
		record.FinishedAt = &finished
	}
	if observe != nil {
		observe(record)
	}
	AddRequestAlias(ctx, "related", record.RequestID)
}

func cloneRequestRecord(record RequestRecord) RequestRecord {
	record.Aliases = append([]RequestAlias(nil), record.Aliases...)
	record.Attempts = append([]RequestAttempt(nil), record.Attempts...)
	record.Timings = maps.Clone(record.Timings)
	return record
}

// MergeRequestRecord 将后台完成事实补入已保存的入口快照。
func MergeRequestRecord(previous, next RequestRecord) RequestRecord {
	if !next.UpdatedAt.After(previous.UpdatedAt) {
		return previous
	}
	next.StartedAt = previous.StartedAt
	if previous.ParentRequestID != "" {
		next.ParentRequestID = previous.ParentRequestID
	}
	if next.Method == "" {
		next.Method = previous.Method
	}
	if next.Path == "" {
		next.Path = previous.Path
	}
	if next.UserID == 0 {
		next.UserID = previous.UserID
	}
	if next.TeamID == 0 {
		next.TeamID = previous.TeamID
	}
	if next.APIKeyID == 0 {
		next.APIKeyID = previous.APIKeyID
	}
	if next.ProviderID == 0 {
		next.ProviderID = previous.ProviderID
	}
	if next.Model == "" {
		next.Model = previous.Model
	}
	if next.Platform == "" {
		next.Platform = previous.Platform
	}
	if next.ErrorCode == "" {
		next.ErrorCode = previous.ErrorCode
	}
	if next.Status == 0 {
		next.Status = previous.Status
	}
	if next.DurationMs == 0 {
		next.DurationMs = previous.DurationMs
	}
	if next.FinishedAt == nil {
		next.FinishedAt = previous.FinishedAt
	}
	if next.Timings == nil {
		next.Timings = previous.Timings
	}
	if len(next.Attempts) == 0 {
		next.Attempts = previous.Attempts
	}
	if previous.State == "failed" || previous.State == "canceled" || next.State == "running" && previous.FinishedAt != nil {
		next.State = previous.State
	}
	seen := make(map[RequestAlias]bool)
	aliases := append(append([]RequestAlias(nil), previous.Aliases...), next.Aliases...)
	next.Aliases = nil
	for _, alias := range aliases {
		if !seen[alias] && len(next.Aliases) < 128 {
			next.Aliases = append(next.Aliases, alias)
			seen[alias] = true
		}
	}
	return next
}

// CloneRequestRecord 复制可变集合，使后台存储和响应脱敏各自持有数据。
func CloneRequestRecord(record RequestRecord) RequestRecord { return cloneRequestRecord(record) }

// WithChildRequest 为同一连接内的逻辑请求建立独立记录。
func WithChildRequest(ctx context.Context, record RequestRecord) context.Context {
	var observe func(RequestRecord)
	if capture, ok := ctx.Value(requestCaptureKey).(*RequestCapture); ok {
		capture.mu.Lock()
		parent := capture.record
		observe = capture.observe
		capture.mu.Unlock()
		record.ParentRequestID = parent.RequestID
		record.UserID = parent.UserID
		record.TeamID = parent.TeamID
		record.APIKeyID = parent.APIKeyID
		record.Method = parent.Method
		record.Path = parent.Path
	} else {
		record.ParentRequestID = RequestIDValue(ctx)
		if record.ParentRequestID == "" {
			record.ParentRequestID = NewRequestID()
		}
	}
	AddRequestAlias(ctx, "related", record.RequestID)
	ctx = context.WithValue(ctx, RequestID, record.RequestID)
	return WithRequestCapture(ctx, record, observe)
}

// IsChildRequest 判断当前 ID 是否已经由连接轮次分配。
func IsChildRequest(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	capture, ok := ctx.Value(requestCaptureKey).(*RequestCapture)
	if !ok {
		return false
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.record.ParentRequestID != ""
}

// NormalizeRequestRecord 限制诊断字段大小，外部 ID 超长时保持缺失。
func NormalizeRequestRecord(record RequestRecord) RequestRecord {
	record = cloneRequestRecord(record)
	record.Model = requestText(record.Model, 200)
	record.Path = requestText(record.Path, 2048)
	record.Method = requestText(record.Method, 32)
	record.ErrorCode = requestText(record.ErrorCode, 64)
	record.Platform = requestText(record.Platform, 64)
	aliases := record.Aliases
	record.Aliases = nil
	seen := make(map[RequestAlias]bool)
	for _, alias := range aliases {
		alias.Value = strings.TrimSpace(alias.Value)
		alias.Kind = requestText(alias.Kind, 32)
		if alias.Value == "" || len(alias.Value) > 255 || strings.ContainsAny(alias.Value, "\r\n\x00") || seen[alias] {
			continue
		}
		seen[alias] = true
		record.Aliases = append(record.Aliases, alias)
		if len(record.Aliases) == 128 {
			break
		}
	}
	if len(record.Attempts) > 256 {
		record.Attempts = record.Attempts[:256]
	}
	for i := range record.Attempts {
		record.Attempts[i].Outcome = requestText(record.Attempts[i].Outcome, 32)
		if len(record.Attempts[i].RequestID) > 255 || strings.ContainsAny(record.Attempts[i].RequestID, "\r\n\x00") {
			record.Attempts[i].RequestID = ""
		}
	}
	return record
}

func requestText(value string, limit int) string {
	value = strings.ToValidUTF8(value, "")
	// PostgreSQL 的文本和 JSONB 字段无法保存空字符，URL 解码后的路径可能包含它。
	value = strings.ReplaceAll(value, "\x00", "")
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
