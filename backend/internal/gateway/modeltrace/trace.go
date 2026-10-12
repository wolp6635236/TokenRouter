package modeltrace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/TokenFlux/TokenRouter/internal/routing"
	"github.com/TokenFlux/TokenRouter/internal/routing/modelmap"
)

// APIKeyModelRedirectTrace 保存一次请求中的客户端模型与内部模型阶段。
type APIKeyModelRedirectTrace struct {
	ClientModel string
	SourceModel string
	TargetModel string

	mu             sync.RWMutex
	responseModels map[string]struct{}
}

// ContextKey 标识 context 中的 Trace，支持参数传递的调用方可直接传入 Trace。
type ContextKey struct{}

// WithGroupRedirect 组合 Key 和分组的模型映射，记录本次请求恢复响应模型时所需的名称。
func WithGroupRedirect(result routing.GroupMappingResult, ctx context.Context, requestedModel string) routing.GroupMappingResult {
	trace, ok := FromContext(ctx)
	if !ok {
		return result
	}
	result.ClientModel = trace.ClientModel
	result.APIKeyRedirected = true
	RegisterStage(ctx, requestedModel)
	RegisterStage(ctx, result.MappedModel)
	return result
}

// RegisterStage 登记当前请求已使用的内部模型，供响应元数据恢复。
func RegisterStage(ctx context.Context, model string) {
	if trace, ok := FromContext(ctx); ok {
		trace.RegisterModel(model)
	}
}

// RewriteAPIKeyAdditionalModels 重定向 Responses 工具声明中的附加模型。
func RewriteAPIKeyAdditionalModels(body []byte, mapping map[string]string) ([]byte, error) {
	if len(body) == 0 || len(mapping) == 0 || !gjson.ValidBytes(body) {
		return body, nil
	}
	rewritten := body
	for index, tool := range gjson.GetBytes(body, "tools").Array() {
		model := strings.TrimSpace(tool.Get("model").String())
		mappedModel, matched := modelmap.Resolve(mapping, model)
		if !matched {
			continue
		}
		var err error
		rewritten, err = sjson.SetBytes(rewritten, fmt.Sprintf("tools.%d.model", index), mappedModel)
		if err != nil {
			return body, err
		}
	}
	return rewritten, nil
}

// NewAPIKeyModelRedirectTrace 创建模型重定向追踪，并登记首个内部目标模型。
func NewAPIKeyModelRedirectTrace(clientModel, sourceModel, targetModel string) *APIKeyModelRedirectTrace {
	trace := &APIKeyModelRedirectTrace{
		ClientModel:    strings.TrimSpace(clientModel),
		SourceModel:    strings.TrimSpace(sourceModel),
		TargetModel:    strings.TrimSpace(targetModel),
		responseModels: make(map[string]struct{}),
	}
	trace.RegisterModel(targetModel)
	return trace
}

func (t *APIKeyModelRedirectTrace) RegisterModel(model string) {
	if t == nil {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" || model == t.SourceModel || model == t.ClientModel {
		return
	}
	t.mu.Lock()
	t.responseModels[model] = struct{}{}
	t.mu.Unlock()
}

// ResponseModels 返回按字典序排列的模型副本，供并发响应写入使用。
func (t *APIKeyModelRedirectTrace) ResponseModels() []string {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	models := make([]string, 0, len(t.responseModels))
	for model := range t.responseModels {
		models = append(models, model)
	}
	t.mu.RUnlock()
	sort.Strings(models)
	return models
}

// ReplaceModelMetadata 替换常见协议的模型元数据字段，正文保持原字节。
func ReplaceModelMetadata(data []byte, fromModel, toModel string) []byte {
	fromModel = strings.TrimSpace(fromModel)
	toModel = strings.TrimSpace(toModel)
	if fromModel == "" || toModel == "" || fromModel == toModel {
		return data
	}
	fromValue, _ := json.Marshal(fromModel)
	toValue, _ := json.Marshal(toModel)
	fromGeminiName, _ := json.Marshal("models/" + fromModel)
	toGeminiName, _ := json.Marshal("models/" + toModel)
	patterns := [][2][]byte{
		{append([]byte(`"model":`), fromValue...), append([]byte(`"model":`), toValue...)},
		{append([]byte(`"model": `), fromValue...), append([]byte(`"model": `), toValue...)},
		{append([]byte(`"modelVersion":`), fromValue...), append([]byte(`"modelVersion":`), toValue...)},
		{append([]byte(`"modelVersion": `), fromValue...), append([]byte(`"modelVersion": `), toValue...)},
		{append([]byte(`"model_version":`), fromValue...), append([]byte(`"model_version":`), toValue...)},
		{append([]byte(`"model_version": `), fromValue...), append([]byte(`"model_version": `), toValue...)},
		{append([]byte(`"id":`), fromValue...), append([]byte(`"id":`), toValue...)},
		{append([]byte(`"id": `), fromValue...), append([]byte(`"id": `), toValue...)},
		{append([]byte(`"name":`), fromGeminiName...), append([]byte(`"name":`), toGeminiName...)},
		{append([]byte(`"name": `), fromGeminiName...), append([]byte(`"name": `), toGeminiName...)},
	}
	rewritten := data
	for _, pattern := range patterns {
		rewritten = bytes.ReplaceAll(rewritten, pattern[0], pattern[1])
	}
	return rewritten
}

// RegisterResponsePayload 从完整 JSON 或 SSE data 事件中登记实际上游模型元数据。
func (t *APIKeyModelRedirectTrace) RegisterResponsePayload(data []byte) {
	if t == nil || len(data) == 0 {
		return
	}
	for _, model := range responseMetadataModels(data) {
		t.RegisterModel(model)
	}
}

// responseMetadataModels 从协议模型字段中收集名称。
func responseMetadataModels(data []byte) []string {
	payloads := make([][]byte, 0, 4)
	trimmed := bytes.TrimSpace(data)
	if json.Valid(trimmed) {
		payloads = append(payloads, trimmed)
	}
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) || !json.Valid(payload) {
			continue
		}
		payloads = append(payloads, payload)
	}

	paths := []string{"model", "modelVersion", "model_version", "response.model", "session.model"}
	models := make([]string, 0, len(payloads))
	seen := make(map[string]struct{})
	appendModel := func(model string) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		if _, exists := seen[model]; exists {
			return
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	for _, payload := range payloads {
		for _, path := range paths {
			value := gjson.GetBytes(payload, path)
			if value.Type == gjson.String {
				appendModel(value.String())
			}
		}
		for _, path := range []string{"name", "response.name"} {
			name := strings.TrimSpace(gjson.GetBytes(payload, path).String())
			if after, ok := strings.CutPrefix(name, "models/"); ok {
				appendModel(after)
			}
		}
	}
	return models
}

func WithContext(ctx context.Context, trace *APIKeyModelRedirectTrace) context.Context {
	if trace == nil {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ContextKey{}, trace)
}

func FromContext(ctx context.Context) (*APIKeyModelRedirectTrace, bool) {
	if ctx == nil {
		return nil, false
	}
	trace, ok := ctx.Value(ContextKey{}).(*APIKeyModelRedirectTrace)
	return trace, ok && trace != nil
}

// Snapshot 在异步交接时复制当前模型链，之后的 attempt/turn 不会污染已提交任务。
func (t *APIKeyModelRedirectTrace) Snapshot() *APIKeyModelRedirectTrace {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := &APIKeyModelRedirectTrace{ClientModel: t.ClientModel, SourceModel: t.SourceModel, TargetModel: t.TargetModel, responseModels: make(map[string]struct{}, len(t.responseModels))}
	for model := range t.responseModels {
		out.responseModels[model] = struct{}{}
	}
	return out
}
