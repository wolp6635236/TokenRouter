package payment

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPaymentAmountToleranceForThreeDecimalCurrency(t *testing.T) {
	t.Parallel()

	assert.Equal(t, ProviderAmountTolerance, PaymentAmountToleranceForCurrency("CNY"))
	assert.Equal(t, ProviderAmountTolerance, PaymentAmountToleranceForCurrency("JPY"))
	assert.InDelta(t, 0.0005, PaymentAmountToleranceForCurrency("KWD"), 1e-12)
}

func TestCalculateCreditedBalanceStillUsesRechargeMultiplier(t *testing.T) {
	t.Parallel()

	got := CalculateCreditedBalance(10, 0.14)
	if got != 1.4 {
		t.Fatalf("credited balance = %v, want 1.4", got)
	}

	got = CalculateCreditedBalance(69.90, 10)
	if got != 699 {
		t.Fatalf("credited balance = %v, want 699", got)
	}
}
