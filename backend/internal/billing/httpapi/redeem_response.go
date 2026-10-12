package httpapi

import (
	"github.com/TokenFlux/TokenRouter/internal/billing"
	billingdto "github.com/TokenFlux/TokenRouter/internal/billing/httpapi/dto"
)

type RedeemCode = billingdto.RedeemCode

type AdminRedeemCode = billingdto.AdminRedeemCode

type BatchUpdateRedeemCodeFields = billingdto.BatchUpdateRedeemCodeFields

type BatchUpdateRedeemCodesRequest = billingdto.BatchUpdateRedeemCodesRequest

// RedeemCodeFromService 将兑换记录转换为用户响应。
func RedeemCodeFromService(rc *billing.RedeemCode) *RedeemCode {
	return billingdto.RedeemCodeFromService(rc)
}

// RedeemCodeFromServiceAdmin 将兑换记录转换为管理员响应。
func RedeemCodeFromServiceAdmin(rc *billing.RedeemCode) *AdminRedeemCode {
	return billingdto.RedeemCodeFromServiceAdmin(rc)
}
