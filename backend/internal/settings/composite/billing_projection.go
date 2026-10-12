package composite

import "github.com/TokenFlux/TokenRouter/internal/billing"

// BillingAdminSettings 从综合快照提取计费设置。
func (s *Snapshot) BillingAdminSettings() billing.AdminSettings {
	return billing.AdminSettings{
		BalanceIconSVG:                  s.BalanceIconSVG,
		BalanceLowNotifyEnabled:         s.BalanceLowNotifyEnabled,
		BalanceLowNotifyRechargeURL:     s.BalanceLowNotifyRechargeURL,
		BalanceLowNotifyThreshold:       s.BalanceLowNotifyThreshold,
		BalanceUnitName:                 s.BalanceUnitName,
		BalanceUnitSymbol:               s.BalanceUnitSymbol,
		DefaultBalance:                  s.DefaultBalance,
		DefaultSubscriptions:            s.DefaultSubscriptions,
		ReasoningPointRMBUnitPrice:      s.ReasoningPointRMBUnitPrice,
		SubscriptionExpiryNotifyEnabled: s.SubscriptionExpiryNotifyEnabled,
		USDExchangeRate:                 s.USDExchangeRate,
	}
}

// ApplyBillingAdminSettings 将计费设置写入综合快照。
func (s *Snapshot) ApplyBillingAdminSettings(value billing.AdminSettings) {
	s.BalanceIconSVG = value.BalanceIconSVG
	s.BalanceLowNotifyEnabled = value.BalanceLowNotifyEnabled
	s.BalanceLowNotifyRechargeURL = value.BalanceLowNotifyRechargeURL
	s.BalanceLowNotifyThreshold = value.BalanceLowNotifyThreshold
	s.BalanceUnitName = value.BalanceUnitName
	s.BalanceUnitSymbol = value.BalanceUnitSymbol
	s.DefaultBalance = value.DefaultBalance
	s.DefaultSubscriptions = value.DefaultSubscriptions
	s.ReasoningPointRMBUnitPrice = value.ReasoningPointRMBUnitPrice
	s.SubscriptionExpiryNotifyEnabled = value.SubscriptionExpiryNotifyEnabled
	s.USDExchangeRate = value.USDExchangeRate
}

// ApplyBillingAdminReadSettings 将余额显示、通知、默认额度和汇率设置写入快照。
func (s *Snapshot) ApplyBillingAdminReadSettings(value *billing.AdminReadSettings) {
	s.BalanceIconSVG = value.BalanceIconSVG
	s.BalanceLowNotifyEnabled = value.BalanceLowNotifyEnabled
	s.BalanceLowNotifyRechargeURL = value.BalanceLowNotifyRechargeURL
	s.BalanceLowNotifyThreshold = value.BalanceLowNotifyThreshold
	s.BalanceUnitName = value.BalanceUnitName
	s.BalanceUnitSymbol = value.BalanceUnitSymbol
	s.DefaultBalance = value.DefaultBalance
	s.DefaultSubscriptions = value.DefaultSubscriptions
	s.ReasoningPointRMBUnitPrice = value.ReasoningPointRMBUnitPrice
	s.SubscriptionExpiryNotifyEnabled = value.SubscriptionExpiryNotifyEnabled
	s.USDExchangeRate = value.USDExchangeRate
}
