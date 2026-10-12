package payment

import (
	"testing"
)

func TestIsOfficialAlipayProviderInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		instance *ProviderInstance
		want     bool
	}{
		{name: "nil instance", instance: nil, want: false},
		{name: "official alipay", instance: &ProviderInstance{ProviderKey: TypeAlipay}, want: true},
		{name: "normalized official alipay", instance: &ProviderInstance{ProviderKey: " ALIPAY "}, want: true},
		{name: "easypay alipay route", instance: &ProviderInstance{ProviderKey: TypeEasyPay}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ConfigIsOfficialAlipayProviderInstance(tt.instance); got != tt.want {
				t.Fatalf("payment.ConfigIsOfficialAlipayProviderInstance() = %v, want %v", got, tt.want)
			}
		})
	}
}
