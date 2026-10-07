package billing

import (
	"strings"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
	"github.com/TokenFlux/TokenRouter/internal/pkg/locale"
)

// PlanCopy 包含套餐对用户展示的完整文案，权益按行保存在 Features 中。
type PlanCopy struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Features    string `json:"features"`
	ProductName string `json:"product_name"`
}

// PlanLocalization 是套餐保存的原文和译文。
type PlanLocalization locale.Content[PlanCopy]

// PlanContent 为单文本接口创建的套餐提供可编辑的内容和初始版本。
func PlanContent(plan *SubscriptionPlan) locale.Content[PlanCopy] {
	if plan.Localization.Revision > 0 {
		return locale.Content[PlanCopy](plan.Localization)
	}
	content := locale.Original(PlanCopy{Name: plan.Name, Description: plan.Description, Features: plan.Features, ProductName: plan.ProductName})
	content.Revision, content.SourceRevision = 1, 1
	return content
}

// ValidatePlanCopy 对所有语言使用同样的名称和内容长度要求。
func ValidatePlanCopy(copy PlanCopy) error {
	if strings.TrimSpace(copy.Name) == "" || len([]rune(copy.Name)) > 100 {
		return apperror.BadRequest("PLAN_NAME_REQUIRED", "Plan name is required and must not exceed 100 characters.")
	}
	if len(copy.Description) > 65536 || len(copy.Features) > 65536 || len([]rune(copy.ProductName)) > 100 {
		return apperror.BadRequest("PLAN_TEXT_TOO_LONG", "Plan text exceeds the allowed length.")
	}
	return nil
}

// LocalizePlan 返回展示副本，计费配置和原文不会被调用方的语言选择覆盖。
func LocalizePlan(plan *SubscriptionPlan, language string) *SubscriptionPlan {
	if plan == nil {
		return nil
	}
	result := *plan
	if plan.Localization.Revision > 0 {
		copy, resolution := locale.Content[PlanCopy](plan.Localization).Resolve(language)
		result.Name, result.Description = copy.Name, copy.Description
		result.Features, result.ProductName = copy.Features, copy.ProductName
		result.Resolution = resolution
	}
	return &result
}
