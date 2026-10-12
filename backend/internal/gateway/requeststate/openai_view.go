package requeststate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/TokenFlux/TokenRouter/internal/protocol/wirejson"
)

type OpenAIRequestView struct {
	body               []byte
	Model              string
	Stream             bool
	PromptCacheKey     string
	PreviousResponseID string
	ServiceTier        string
	HasServiceTier     bool
	ReasoningEffort    string
	patches            []openAIRequestPatch
	patchesDisabled    bool
}

type openAIRequestPatch struct {
	path   string
	delete bool
	value  any
}

// DecodeOpenAIRequestBody 解码请求体，保留数字精度并为错误添加解析前缀。
func DecodeOpenAIRequestBody(body []byte) (map[string]any, error) {
	var request map[string]any
	if err := wirejson.DecodeUseNumber(body, &request); err != nil {
		return nil, fmt.Errorf("parse request: %w", err)
	}
	return request, nil
}

func NewOpenAIRequestView(body []byte) OpenAIRequestView {
	if len(body) == 0 {
		return OpenAIRequestView{}
	}
	const (
		modelField uint8 = 1 << iota
		streamField
		promptCacheKeyField
		previousResponseIDField
		serviceTierField
		reasoningField
		allRequestViewFields = modelField | streamField | promptCacheKeyField |
			previousResponseIDField | serviceTierField | reasoningField
	)

	view := OpenAIRequestView{body: body}
	var seen uint8
	// 视图持有 body 并直接引用原始请求体，input 和 contents 使用其中的字节区间。
	wirejson.ParseView(body).ForEach(func(key, value gjson.Result) bool {
		switch key.Str {
		case "model":
			if seen&modelField == 0 {
				view.Model = strings.TrimSpace(value.String())
				seen |= modelField
			}
		case "stream":
			if seen&streamField == 0 {
				view.Stream = value.Bool()
				seen |= streamField
			}
		case "prompt_cache_key":
			if seen&promptCacheKeyField == 0 {
				view.PromptCacheKey = strings.TrimSpace(value.String())
				seen |= promptCacheKeyField
			}
		case "previous_response_id":
			if seen&previousResponseIDField == 0 {
				view.PreviousResponseID = strings.TrimSpace(value.String())
				seen |= previousResponseIDField
			}
		case "service_tier":
			if seen&serviceTierField == 0 {
				view.ServiceTier = strings.TrimSpace(value.String())
				view.HasServiceTier = value.Exists()
				seen |= serviceTierField
			}
		case "reasoning":
			if seen&reasoningField == 0 {
				view.ReasoningEffort = strings.TrimSpace(value.Get("effort").String())
				seen |= reasoningField
			}
		}
		return seen != allRequestViewFields
	})
	return view
}

// Decode 在调用方确实需要完整对象时解码视图当前持有的报文。
func (v OpenAIRequestView) Decode() (map[string]any, error) {
	return DecodeOpenAIRequestBody(v.Bytes())
}

// OpenAIRequestMetaFromBody 通过字段视图提取模型、流式标记和缓存键。
func OpenAIRequestMetaFromBody(body []byte) (model string, stream bool, promptCacheKey string) {
	view := NewOpenAIRequestView(body)
	return view.Model, view.Stream, view.PromptCacheKey
}

func (v *OpenAIRequestView) MarkPatchSet(path string, value any) {
	if v == nil || v.patchesDisabled {
		return
	}
	path = strings.TrimSpace(path)
	if !isSimpleOpenAIRequestPatchPath(path) {
		v.DisablePatches()
		return
	}
	v.patches = append(v.patches, openAIRequestPatch{path: path, value: value})
}

func (v *OpenAIRequestView) MarkPatchDelete(path string) {
	if v == nil || v.patchesDisabled {
		return
	}
	path = strings.TrimSpace(path)
	if !isSimpleOpenAIRequestPatchPath(path) {
		v.DisablePatches()
		return
	}
	v.patches = append(v.patches, openAIRequestPatch{path: path, delete: true})
}

func isSimpleOpenAIRequestPatchPath(path string) bool {
	if path == "" || strings.ContainsRune(path, '\\') {
		return false
	}
	for part := range strings.SplitSeq(path, ".") {
		if strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}

func (v *OpenAIRequestView) DisablePatches() {
	if v == nil {
		return
	}
	v.patchesDisabled = true
	v.patches = nil
}

func (v OpenAIRequestView) HasPatches() bool {
	return !v.patchesDisabled && len(v.patches) > 0
}

func (v OpenAIRequestView) ApplyPatches() ([]byte, error) {
	if v.patchesDisabled || len(v.patches) == 0 {
		return nil, errors.New("openai request patches disabled")
	}
	body := v.body
	for _, patch := range v.patches {
		var err error
		if patch.delete {
			body, err = sjson.DeleteBytes(body, patch.path)
		} else {
			body, err = sjson.SetBytes(body, patch.path, patch.value)
		}
		if err != nil {
			return nil, err
		}
	}
	return body, nil
}

// Bytes 返回视图持有的原始报文，不复制大 input。
func (v OpenAIRequestView) Bytes() []byte { return v.body }

// PatchesDisabled 区分局部改写与已进入完整解码的路径。
func (v OpenAIRequestView) PatchesDisabled() bool { return v.patchesDisabled }
