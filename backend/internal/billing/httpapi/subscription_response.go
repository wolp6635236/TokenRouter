package httpapi

import (
	"github.com/TokenFlux/TokenRouter/internal/billing"
	billingdto "github.com/TokenFlux/TokenRouter/internal/billing/httpapi/dto"
)

type UserSubscription = billingdto.UserSubscription

type AdminUserSubscription = billingdto.AdminUserSubscription

type BulkAssignResult = billingdto.BulkAssignResult

// DefaultSubscriptionSetting 是默认订阅设置的 HTTP JSON 数据。
type DefaultSubscriptionSetting struct {
	PlanID int64 `json:"plan_id"`
}

// UserSubscriptionFromService 转换订阅响应，并按指定语言翻译套餐资料。
func UserSubscriptionFromService(sub *billing.UserSubscription, language ...string) *UserSubscription {
	if sub != nil && len(language) > 0 {
		copy := *sub
		copy.Plan = billing.LocalizePlan(sub.Plan, language[0])
		return billingdto.UserSubscriptionFromService(&copy)
	}
	return billingdto.UserSubscriptionFromService(sub)
}

// UserSubscriptionFromServiceAdmin 将订阅转换为管理员响应。
func UserSubscriptionFromServiceAdmin(sub *billing.UserSubscription) *AdminUserSubscription {
	return billingdto.UserSubscriptionFromServiceAdmin(sub)
}

// BulkAssignResultFromService 将批量发放结果转换为响应。
func BulkAssignResultFromService(r *billing.BulkAssignResult) *BulkAssignResult {
	return billingdto.BulkAssignResultFromService(r)
}
