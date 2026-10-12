package payment_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/TokenFlux/TokenRouter/internal/payment"
	paymenttestkit "github.com/TokenFlux/TokenRouter/internal/payment/testkit"
)

func TestExpectedNotificationProviderKeyForOrderUsesSnapshotProviderKey(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymenttestkit.StaticProvider{
		Key:   payment.TypeAlipay,
		Types: []payment.PaymentType{payment.TypeAlipay},
	})

	order := &payment.Order{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version": 1,
			"provider_key":   payment.TypeEasyPay,
		},
	}

	assert.Equal(t,
		payment.TypeEasyPay, payment.ExpectedNotificationProviderKeyForOrder(registry, order, ""),
	)
}
