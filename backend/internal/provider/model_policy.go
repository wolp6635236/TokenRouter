package provider

import (
	"sort"
	"strings"
)

// ModelPlatformRules 只在平台专有资格分支按需调用，模型配置与一跳匹配由 provider 拥有。
type ModelPlatformRules struct {
	NormalizeQoder      func(string) string
	QoderCompatible     func(string) bool
	OpenAIOAuthServable func(string) bool
}

// IsModelSupported 在配置的白名单或默认目录中检查最终模型，提供商映射执行一次。
func (a *Record) IsModelSupported(requestedModel string, defaults ModelMappingDefaults, rules ModelPlatformRules) bool {
	if a == nil {
		return false
	}
	mapping := ResolveModelMapping(a, defaults)
	model, _ := ResolveMappedModel(mapping, requestedModel)
	scope := a.effectiveModelScope(defaults, mapping)
	if !ModelInFinalWhitelist(a.Platform, model, scope, rules.NormalizeQoder) {
		return false
	}
	if a.Platform == PlatformQoder && rules.QoderCompatible != nil && !rules.QoderCompatible(model) {
		return false
	}
	if a.IsOpenAIOAuth() && rules.OpenAIOAuthServable != nil {
		return rules.OpenAIOAuthServable(model)
	}
	return true
}

// FinalModelWhitelisted 直接检查已映射的上游模型名是否在白名单中。
func (a *Record) FinalModelWhitelisted(model string, defaults ModelMappingDefaults, rules ModelPlatformRules) bool {
	if a == nil {
		return false
	}
	if a.Platform == PlatformQoder && rules.QoderCompatible != nil && !rules.QoderCompatible(model) {
		return false
	}
	return ModelInFinalWhitelist(a.Platform, model, a.effectiveModelScope(defaults, ResolveModelMapping(a, defaults)), rules.NormalizeQoder)
}

// effectiveModelScope 空配置使用默认目录，明确的非空白名单覆盖默认值。
func (a *Record) effectiveModelScope(defaults ModelMappingDefaults, mapping map[string]string) map[string]struct{} {
	whitelist, explicit := ResolveFinalModelWhitelist(a.Platform, a.Credentials, mapping)
	// Spark 影子只有独立模型配额，明确的通配符也不能扩大其硬能力。
	if a.IsShadow() && a.Platform == PlatformOpenAI {
		scope := make(map[string]struct{})
		if defaults.Models != nil {
			for _, model := range defaults.Models(a) {
				if len(whitelist) == 0 || ModelInFinalWhitelist(a.Platform, model, whitelist, nil) {
					scope[model] = struct{}{}
				}
			}
		}
		return scope
	}
	if explicit && len(whitelist) > 0 {
		return whitelist
	}
	scope := make(map[string]struct{})
	if defaults.Models != nil {
		for _, model := range defaults.Models(a) {
			scope[strings.TrimSpace(model)] = struct{}{}
		}
	}
	// 明确映射的目标是管理员声明的能力；通配目标不能隐式放开全部模型。
	for _, model := range mapping {
		if model != "" && !strings.Contains(model, "*") {
			scope[strings.TrimSpace(model)] = struct{}{}
		}
	}
	return scope
}

// GetConfiguredRequestModels 枚举默认目录和明确模型；通配符仅用于资格匹配。
func (a *Record) GetConfiguredRequestModels(defaults ModelMappingDefaults) []string {
	if a == nil {
		return nil
	}
	mapping := ResolveModelMapping(a, defaults)
	scope := a.effectiveModelScope(defaults, mapping)
	models := make(map[string]struct{})
	for model := range scope {
		if !strings.Contains(model, "*") {
			models[model] = struct{}{}
		}
	}
	if defaults.Models != nil {
		for _, model := range defaults.Models(a) {
			if ModelInFinalWhitelist(a.Platform, model, scope, nil) {
				models[model] = struct{}{}
			}
		}
	}
	for source, target := range mapping {
		if !strings.Contains(source, "*") && ModelInFinalWhitelist(a.Platform, target, scope, nil) {
			models[source] = struct{}{}
		}
	}
	out := make([]string, 0, len(models))
	for model := range models {
		out = append(out, model)
	}
	sort.Strings(out)
	return out
}

// ResolveMappedModel 获取映射后的模型名，并返回是否命中了提供商级映射。
// matched=true 表示命中了精确映射或通配符映射，即使映射结果与原模型名相同。
func ResolveMappedModel(mapping map[string]string, requestedModel string) (mappedModel string, matched bool) {
	if len(mapping) == 0 {
		return requestedModel, false
	}
	if mappedModel, matched := ResolveRequestedModelInMapping(mapping, requestedModel); matched {
		return mappedModel, true
	}
	normalized := strings.TrimSpace(requestedModel)
	if normalized != requestedModel {
		if mappedModel, matched := ResolveRequestedModelInMapping(mapping, normalized); matched {
			return mappedModel, true
		}
	}
	return requestedModel, false
}

// ModelInFinalWhitelist 检查最终模型是否命中白名单，Qoder 按路由键比较别名。
func ModelInFinalWhitelist(platform, model string, whitelist map[string]struct{}, normalizeQoder func(string) string) bool {
	if len(whitelist) == 0 {
		return false
	}
	model = strings.TrimSpace(model)
	for pattern := range whitelist {
		pattern = strings.TrimSpace(pattern)
		if strings.EqualFold(model, pattern) || (strings.HasSuffix(pattern, "*") && strings.HasPrefix(strings.ToLower(model), strings.ToLower(strings.TrimSuffix(pattern, "*")))) {
			return true
		}
		if platform == PlatformQoder && normalizeQoder != nil && normalizeQoder(pattern) == normalizeQoder(model) {
			return true
		}
	}
	return false
}

// HasUnrestrictedModelScope 只有管理员明确配置全模型范围才返回真。
func (a *Record) HasUnrestrictedModelScope(defaults ModelMappingDefaults) bool {
	if a == nil {
		return false
	}
	_, all := a.effectiveModelScope(defaults, ResolveModelMapping(a, defaults))["*"]
	return all
}
