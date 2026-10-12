package tierpolicy

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/TokenFlux/TokenRouter/internal/apikey"
	"github.com/TokenFlux/TokenRouter/internal/protocol/openai"
	"github.com/TokenFlux/TokenRouter/internal/routing"
)

// DecisionInput 保存平台和分组，按评估顺序按需读取设置和价格能力。
type DecisionInput struct {
	Model            string
	GroupPolicy      string
	OpenAI           bool
	Evaluate         func(string) (string, string)
	KeyPolicy        func() string
	ForceOnSupported func() bool
}

// Decision 描述最终应写入、删除或拒绝的 OpenAI Fast 决策。
type Decision struct {
	Tier        string
	DeleteField bool
	Blocked     *BlockedError
}

// IsAcceleratedTier 同时覆盖 Fast 和 Ultra Fast。
func IsAcceleratedTier(tier string) bool {
	return tier == OpenAIFastTierPriority || tier == OpenAIFastTierUltrafast
}

// Resolve 解析系统策略和单 Key 策略。
// 先对请求档位应用系统策略，Key 改写后再次应用系统策略，使 filter/block 对 force_on 结果也生效。
func Resolve(input DecisionInput, rawTier string, hasField bool) Decision {
	normTier := openai.ServiceTierValue(rawTier)
	groupPolicy := input.GroupPolicy
	switch groupPolicy {
	case routing.GroupOpenAIFastPolicyForcePriority:
		normTier, hasField = OpenAIFastTierPriority, true
	case routing.GroupOpenAIFastPolicyForceUltrafast:
		normTier, hasField = OpenAIFastTierUltrafast, true
	}
	applySystemAction := func(tier string) (Decision, bool) {
		action, errMsg := input.Evaluate(tier)
		switch action {
		case "block":
			if errMsg == "" {
				errMsg = fmt.Sprintf("openai service_tier=%s is not allowed for model %s", tier, input.Model)
			}
			return Decision{Blocked: &BlockedError{Message: errMsg}}, true
		case "filter":
			return Decision{DeleteField: true}, true
		case OpenAIFastPolicyActionForcePriority:
			return Decision{Tier: OpenAIFastTierPriority}, true
		case OpenAIFastPolicyActionForceUltrafast:
			return Decision{Tier: OpenAIFastTierUltrafast}, true
		default:
			return Decision{}, false
		}
	}

	// 原始请求已命中的非 pass 系统动作直接生效，Key 策略不能覆盖。
	if normTier != "" {
		if decision, handled := applySystemAction(normTier); handled {
			return decision
		}
	}

	// 全局先裁决；分组关闭后，单 Key 不得重新开启。
	if groupPolicy == routing.GroupOpenAIFastPolicyForceOff {
		if IsAcceleratedTier(normTier) {
			return Decision{DeleteField: hasField}
		}
		return Decision{Tier: normTier}
	}
	candidateTier := normTier
	policy := input.KeyPolicy()
	keyPolicyApplicable := false
	switch policy {
	case apikey.APIKeyFastModePolicyForceOn:
		keyPolicyApplicable = input.ForceOnSupported()
	case apikey.APIKeyFastModePolicyForceOff:
		// 强制关闭时清除 priority，其他档位保持原值。
		keyPolicyApplicable = input.OpenAI
	}
	candidateChanged := false
	if keyPolicyApplicable {
		switch policy {
		case apikey.APIKeyFastModePolicyForceOn:
			// 单 Key 开启 Fast 不降低分组强制的 Ultra Fast。
			if groupPolicy != routing.GroupOpenAIFastPolicyForceUltrafast {
				candidateTier = OpenAIFastTierPriority
			}
		case apikey.APIKeyFastModePolicyForceOff:
			// flex 是低优先级模式，auto/default/scale 也是官方合法 tier，均需保留。
			if IsAcceleratedTier(normTier) {
				candidateTier = ""
			}
		}
		candidateChanged = candidateTier != normTier
	}

	// Key 注入或改写出的 tier 必须重新接受系统策略裁决。
	if candidateTier != "" {
		if candidateChanged {
			if decision, handled := applySystemAction(candidateTier); handled {
				return decision
			}
		}
		return Decision{Tier: candidateTier}
	}
	if policy == apikey.APIKeyFastModePolicyForceOff && keyPolicyApplicable && IsAcceleratedTier(normTier) {
		return Decision{DeleteField: hasField}
	}
	return Decision{}
}

// ApplyBody 对请求体应用系统和单 Key Fast 策略，并规范化 service_tier。
// Chat 和 Messages 入口此前已规范化，透传和 Responses 入口由此处完成。
// fast 需要转换为 priority，上游直接收到 fast 会返回 400。
func ApplyBody(body []byte, input DecisionInput) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	tierResult := gjson.GetBytes(body, "service_tier")
	decision := Resolve(input, tierResult.String(), tierResult.Exists())
	if decision.Blocked != nil {
		return body, decision.Blocked
	}
	if decision.DeleteField {
		trimmed, err := sjson.DeleteBytes(body, "service_tier")
		if err != nil {
			return body, fmt.Errorf("strip service_tier from body: %w", err)
		}
		return trimmed, nil
	}
	if decision.Tier != "" && (!tierResult.Exists() || decision.Tier != tierResult.String()) {
		updated, err := sjson.SetBytes(body, "service_tier", decision.Tier)
		if err != nil {
			return body, fmt.Errorf("apply service_tier to body: %w", err)
		}
		return updated, nil
	}
	return body, nil
}
