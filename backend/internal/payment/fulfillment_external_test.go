package payment_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/billing"
	"github.com/TokenFlux/TokenRouter/internal/payment"
	paymenttestkit "github.com/TokenFlux/TokenRouter/internal/payment/testkit"
	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
)

// Callback association uses merchant-order lookup outcome, saved provider
// identity and reported merchant metadata before paid money can enter
// fulfillment. These pure external-package tests exercise those decision
// helpers with immutable parallel fixtures. Redeem-action cases distinguish
// lookup failure, previously used code and existing unused code, while rebate
// cases use credited balance or configured subscription quotas as their
// asserted base. Executor cases record storage calls to check status admission,
// subscription plan validation, and the dispatcher's separate eligibility read.

// fulfillmentEntryStore records order reads and lease attempts at public
// fulfillment entries.
type fulfillmentEntryStore struct {
	// FulfillmentStore remains nil so any persistence beyond the two configured
	// entry operations fails the test instead of silently changing fixture state.
	payment.FulfillmentStore
	// order and claim supply each test's observations and record calls in local
	// counters or traces. Tests invoke the fixture synchronously and inspect
	// those observations after the entry returns.
	order func(context.Context, int64) (*payment.Order, error)            // configured order read callback
	claim func(context.Context, int64, time.Time, time.Time) (int, error) // configured lease attempt callback
}

// TestExecuteFulfillmentEntriesEligibility compares public balance and
// subscription entries across stored statuses and plan identities. It preserves
// status-first errors, completed no-ops, and plan validation before lease attempts.
func TestExecuteFulfillmentEntriesEligibility(t *testing.T) {
	t.Parallel()

	leaseErr := errors.New("lease storage unavailable")
	statuses := []struct {
		status  string // persisted order status
		message string // expected rejection message
		claim   bool   // eligibility for lease attempt
	}{
		{status: payment.OrderStatusCompleted},
		{status: payment.OrderStatusPaid, claim: true},
		{status: payment.OrderStatusFailed, claim: true},
		{status: payment.OrderStatusRecharging, claim: true},
		{status: payment.OrderStatusPending, message: "order cannot fulfill in status PENDING"},
		{status: payment.OrderStatusProcessing, message: "order cannot fulfill in status PROCESSING"},
		{status: payment.OrderStatusExpired, message: "order cannot fulfill in status EXPIRED"},
		{status: payment.OrderStatusCancelled, message: "order cannot fulfill in status CANCELLED"},
		{status: payment.OrderStatusRefundRequested, message: "refund-related order cannot fulfill"},
		{status: payment.OrderStatusRefunding, message: "refund-related order cannot fulfill"},
		{status: payment.OrderStatusRefundPending, message: "refund-related order cannot fulfill"},
		{status: payment.OrderStatusPartiallyRefunded, message: "refund-related order cannot fulfill"},
		{status: payment.OrderStatusRefunded, message: "refund-related order cannot fulfill"},
		{status: payment.OrderStatusRefundFailed, message: "refund-related order cannot fulfill"},
		{status: "UNKNOWN", message: "order cannot fulfill in status UNKNOWN"},
		{message: "order cannot fulfill in status "},
	}
	plans := []struct {
		name string // named plan identity case
		id   *int64 // supplied subscription plan identity
	}{
		{name: "missing"},
		{name: "zero", id: new(int64(0))},
		{name: "negative", id: new(int64(-1))},
		{name: "positive", id: new(int64(7))},
	}
	for _, orderType := range []string{payment.OrderTypeBalance, payment.OrderTypeSubscription} {
		for _, status := range statuses {
			for _, plan := range plans {
				t.Run(orderType+"/"+status.status+"/"+plan.name, func(t *testing.T) {
					var calls []string
					order := &payment.Order{ID: 41, OrderType: orderType, Status: status.status, PlanID: plan.id}
					store := &fulfillmentEntryStore{
						order: func(_ context.Context, id int64) (*payment.Order, error) {
							require.Equal(t, order.ID, id)
							calls = append(calls, "read")
							return order, nil
						},
						claim: func(_ context.Context, id int64, _, _ time.Time) (int, error) {
							require.Equal(t, order.ID, id)
							calls = append(calls, "claim")
							return 0, leaseErr
						},
					}
					svc := payment.NewFulfillment(store, nil, nil, nil, nil, payment.FulfillmentRuntime{})
					execute := svc.ExecuteBalanceFulfillment
					if orderType == payment.OrderTypeSubscription {
						execute = svc.ExecuteSubscriptionFulfillment
					}
					err := execute(t.Context(), order.ID)
					wantMessage := status.message
					wantClaim := status.claim
					if wantClaim && orderType == payment.OrderTypeSubscription && (plan.id == nil || *plan.id <= 0) {
						wantMessage = "missing subscription info"
						wantClaim = false
					}
					switch {
					case wantClaim:
						require.ErrorIs(t, err, leaseErr)
						require.EqualError(t, err, "acquire fulfillment lease: lease storage unavailable")
						require.Equal(t, []string{"read", "claim"}, calls)
					case wantMessage != "":
						require.Equal(t, apperror.CategoryBadRequest, apperror.CategoryOf(err))
						require.Equal(t, "INVALID_STATUS", apperror.Reason(err))
						require.Equal(t, wantMessage, apperror.Message(err))
						require.Equal(t, []string{"read"}, calls)
					default:
						require.NoError(t, err)
						require.Equal(t, []string{"read"}, calls)
					}
					require.Nil(t, order.PaidAt)
					require.Equal(t, status.status, order.Status)
				})
			}
		}
	}
}

// TestExecuteFulfillmentEntriesReadErrors supplies storage absence and arbitrary
// read failures to both public executors. Each error becomes NOT_FOUND before
// status or plan checks, without exposing the original cause.
func TestExecuteFulfillmentEntriesReadErrors(t *testing.T) {
	t.Parallel()

	for _, orderType := range []string{payment.OrderTypeBalance, payment.OrderTypeSubscription} {
		for _, readErr := range []error{payment.ErrOrderNotFound, errors.New("database unavailable")} {
			for _, order := range []*payment.Order{nil, {ID: 41, Status: payment.OrderStatusCompleted}} {
				t.Run(orderType+"/"+readErr.Error()+"/"+fmt.Sprint(order != nil), func(t *testing.T) {
					reads := 0
					store := &fulfillmentEntryStore{
						order: func(_ context.Context, id int64) (*payment.Order, error) {
							require.Equal(t, int64(41), id)
							reads++
							return order, readErr
						},
					}
					svc := payment.NewFulfillment(store, nil, nil, nil, nil, payment.FulfillmentRuntime{})
					execute := svc.ExecuteBalanceFulfillment
					if orderType == payment.OrderTypeSubscription {
						execute = svc.ExecuteSubscriptionFulfillment
					}
					err := execute(t.Context(), 41)
					require.Equal(t, apperror.CategoryNotFound, apperror.CategoryOf(err))
					require.Equal(t, "NOT_FOUND", apperror.Reason(err))
					require.Equal(t, "order not found", apperror.Message(err))
					require.NotErrorIs(t, err, readErr)
					require.Equal(t, 1, reads)
				})
			}
		}
	}
}

// TestExecuteFulfillmentDispatchReads changes the order between dispatch and
// executor reads and injects failures at each boundary. It protects the second
// observation, error conversion boundary, and subscription-only plan requirement.
func TestExecuteFulfillmentDispatchReads(t *testing.T) {
	t.Parallel()

	readErr := errors.New("database unavailable")
	leaseErr := errors.New("lease storage unavailable")
	tests := []struct {
		name         string // named dispatch read scenario
		firstStatus  string // status seen by dispatcher
		secondStatus string // status seen by executor
		readErrorAt  int    // failing read or zero
		wantReads    int    // expected order read count
		wantClaim    bool   // expected lease attempt presence
	}{
		{name: "first_read_error", readErrorAt: 1, wantReads: 1},
		{name: "second_read_error", firstStatus: payment.OrderStatusPaid, readErrorAt: 2, wantReads: 2},
		{name: "completed_during_dispatch", firstStatus: payment.OrderStatusPaid, secondStatus: payment.OrderStatusCompleted, wantReads: 2},
		{name: "paid_after_dispatch", firstStatus: payment.OrderStatusCompleted, secondStatus: payment.OrderStatusPaid, wantReads: 2, wantClaim: true},
	}
	for _, orderType := range []string{payment.OrderTypeBalance, payment.OrderTypeSubscription, "UNKNOWN"} {
		for _, tt := range tests {
			t.Run(orderType+"/"+tt.name, func(t *testing.T) {
				reads := 0
				var calls []string
				store := &fulfillmentEntryStore{
					order: func(_ context.Context, id int64) (*payment.Order, error) {
						require.Equal(t, int64(41), id)
						reads++
						calls = append(calls, "read")
						if reads == tt.readErrorAt {
							return nil, readErr
						}
						status := tt.firstStatus
						if reads > 1 {
							status = tt.secondStatus
						}
						return &payment.Order{ID: id, OrderType: orderType, Status: status}, nil
					},
					claim: func(_ context.Context, id int64, _, _ time.Time) (int, error) {
						require.Equal(t, int64(41), id)
						calls = append(calls, "claim")
						return 0, leaseErr
					},
				}
				svc := payment.NewFulfillment(store, nil, nil, nil, nil, payment.FulfillmentRuntime{})
				err := svc.ExecuteFulfillment(t.Context(), 41)
				switch {
				case tt.readErrorAt == 1:
					require.ErrorIs(t, err, readErr)
					require.EqualError(t, err, "get order: database unavailable")
				case tt.readErrorAt == 2:
					require.Equal(t, apperror.CategoryNotFound, apperror.CategoryOf(err))
					require.Equal(t, "NOT_FOUND", apperror.Reason(err))
					require.Equal(t, "order not found", apperror.Message(err))
					require.NotErrorIs(t, err, readErr)
				case tt.wantClaim && orderType == payment.OrderTypeSubscription:
					require.Equal(t, apperror.CategoryBadRequest, apperror.CategoryOf(err))
					require.Equal(t, "INVALID_STATUS", apperror.Reason(err))
					require.Equal(t, "missing subscription info", apperror.Message(err))
				case tt.wantClaim:
					require.ErrorIs(t, err, leaseErr)
				default:
					require.NoError(t, err)
				}
				require.Equal(t, tt.wantReads, reads)
				wantCalls := []string{"read"}
				if tt.wantReads == 2 {
					wantCalls = append(wantCalls, "read")
				}
				if tt.wantClaim && orderType != payment.OrderTypeSubscription {
					wantCalls = append(wantCalls, "claim")
				}
				require.Equal(t, wantCalls, calls)
			})
		}
	}
}

// Order delegates the configured read for the supplied local order ID.
func (s *fulfillmentEntryStore) Order(ctx context.Context, id int64) (*payment.Order, error) {
	return s.order(ctx, id)
}

// ClaimFulfillment delegates the configured lease attempt with its timestamps.
func (s *fulfillmentEntryStore) ClaimFulfillment(ctx context.Context, id int64, now, staleBefore time.Time) (int, error) {
	return s.claim(ctx, id, now, staleBefore)
}

// TestParseLegacyPaymentOrderID supplies early numeric, current and malformed
// callback labels with true and false notFound flags. It permits positive
// sub2_ identities when that flag is true and rejects the other listed forms
// or a false lookup-absence flag.
func TestParseLegacyPaymentOrderID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string // named identifier case
		orderID string // incoming callback order label
		// notFound permits legacy parsing only for the classified exact-lookup
		// absence; generic lookup failures leave fallback disabled.
		notFound bool  // exact lookup absence flag
		wantID   int64 // expected local numeric identity
		wantOK   bool  // expected legacy recognition result
	}{
		{name: "早期订单", orderID: "sub2_42", notFound: true, wantID: 42, wantOK: true},
		{name: "早期订单带空白", orderID: " sub2_42 ", notFound: true, wantID: 42, wantOK: true},
		{name: "查询错误", orderID: "sub2_42"},
		{name: "当前前缀", orderID: "tr_42", notFound: true},
		{name: "当前完整订单号", orderID: "tr_2026100912345678", notFound: true},
		{name: "历史随机订单号", orderID: "sub2_20261009abcd1234", notFound: true},
		{name: "缺少前缀", orderID: "42", notFound: true},
		{name: "空编号", orderID: "sub2_", notFound: true},
		{name: "零编号", orderID: "sub2_0", notFound: true},
		{name: "负编号", orderID: "sub2_-42", notFound: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotID, gotOK := payment.ParseLegacyPaymentOrderID(tt.orderID, tt.notFound)
			require.Equal(t, tt.wantOK, gotOK)
			require.Equal(t, tt.wantID, gotID)
		})
	}
}

// TestResolveRedeemAction_CodeNotFound supplies no code and no lookup error to
// model an absent prior grant. It expects the creation decision from the
// classifier before any redemption is performed.
func TestResolveRedeemAction_CodeNotFound(t *testing.T) {
	t.Parallel()
	action := payment.ResolveRedeemAction(nil, nil)
	assert.Equal(t, payment.RedeemActionCreate, action, "nil code with nil error should create")
}

// TestResolveRedeemAction_LookupError supplies a database lookup error with no
// code. It expects the creation decision, documenting that this classifier
// treats the lookup failure as requiring a create attempt.
func TestResolveRedeemAction_LookupError(t *testing.T) {
	t.Parallel()
	action := payment.ResolveRedeemAction(nil, errors.New("db connection lost"))
	assert.Equal(t, payment.RedeemActionCreate, action, "lookup error should fall back to create")
}

// TestResolveRedeemAction_LookupErrorWithNonNilCode supplies an unused code
// together with a lookup error. It expects creation because the error takes
// precedence over the simultaneously returned code observation.
func TestResolveRedeemAction_LookupErrorWithNonNilCode(t *testing.T) {
	t.Parallel()
	// Supply both a code and an error to expose precedence; returning a code
	// alongside failure still selects another creation attempt.
	code := &billing.RedeemCode{Status: billing.StatusUnused}
	action := payment.ResolveRedeemAction(code, errors.New("partial error"))
	assert.Equal(t, payment.RedeemActionCreate, action, "non-nil error should always result in create regardless of code")
}

// TestResolveRedeemAction_CodeExistsAndUsed supplies a used balance code with
// its sole redemption consumed. It expects completion skipping from the action
// classifier based on this prior-use fixture.
func TestResolveRedeemAction_CodeExistsAndUsed(t *testing.T) {
	t.Parallel()
	code := &billing.RedeemCode{
		Code:      "test-code-123",
		Status:    billing.StatusUsed,
		Type:      billing.RedeemTypeBalance,
		Value:     10.0,
		MaxUses:   1,
		UsedCount: 1,
	}
	action := payment.ResolveRedeemAction(code, nil)
	assert.Equal(t, payment.RedeemActionSkipCompleted, action, "used code should skip to completed")
}

// TestResolveRedeemAction_CodeExistsAndUnused supplies an existing unused
// balance code after a successful lookup. It expects redemption of that code
// as the next action, bypassing another creation decision.
func TestResolveRedeemAction_CodeExistsAndUnused(t *testing.T) {
	t.Parallel()
	code := &billing.RedeemCode{
		Code:   "test-code-456",
		Status: billing.StatusUnused,
		Type:   billing.RedeemTypeBalance,
		Value:  25.0,
	}
	action := payment.ResolveRedeemAction(code, nil)
	assert.Equal(t, payment.RedeemActionRedeem, action, "unused code should skip creation and proceed to redeem")
}

// TestResolveRedeemAction_CodeExistsWithExpiredStatus supplies an existing
// expired code to the action selector. It still expects redemption selection,
// leaving actual status rejection to the later redemption implementation.
func TestResolveRedeemAction_CodeExistsWithExpiredStatus(t *testing.T) {
	t.Parallel()
	// Use expired status as a non-used code observation; the action selector
	// delegates actual status acceptance to the redemption rules.
	code := &billing.RedeemCode{
		Code:   "expired-code",
		Status: billing.StatusExpired,
	}
	action := payment.ResolveRedeemAction(code, nil)
	assert.Equal(t, payment.RedeemActionRedeem, action, "expired-status code is not IsUsed(), should redeem")
}

// TestResolveRedeemAction_Table compares absent, used and unused codes with
// successful or failed lookups. Each immutable parallel case asserts the
// action, including error precedence over an otherwise used code.
func TestResolveRedeemAction_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// name identifies each case, while code and err jointly supply the lookup
		// observation; expected records the next action selected from their
		// combination.
		name string              // named redemption action case
		code *billing.RedeemCode // supplied prior code observation
		// err takes precedence over a returned code whenever nonnil, including a
		// code whose status would otherwise skip completion.
		err      error                // supplied code lookup error
		expected payment.RedeemAction // expected next redemption action
	}{
		{
			name:     "nil code, nil error — first run",
			code:     nil,
			err:      nil,
			expected: payment.RedeemActionCreate,
		},
		{
			name:     "nil code, lookup error — treat as not found",
			code:     nil,
			err:      billing.ErrRedeemCodeNotFound,
			expected: payment.RedeemActionCreate,
		},
		{
			name:     "nil code, generic DB error — treat as not found",
			code:     nil,
			err:      errors.New("connection refused"),
			expected: payment.RedeemActionCreate,
		},
		{
			name:     "code exists, used — previous run completed redeem",
			code:     &billing.RedeemCode{Status: billing.StatusUsed, MaxUses: 1, UsedCount: 1},
			err:      nil,
			expected: payment.RedeemActionSkipCompleted,
		},
		{
			name:     "code exists, unused — previous run created code but crashed before redeem",
			code:     &billing.RedeemCode{Status: billing.StatusUnused},
			err:      nil,
			expected: payment.RedeemActionRedeem,
		},
		{
			name:     "code exists but error also set — error takes precedence",
			code:     &billing.RedeemCode{Status: billing.StatusUsed, MaxUses: 1, UsedCount: 1},
			err:      errors.New("unexpected"),
			expected: payment.RedeemActionCreate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := payment.ResolveRedeemAction(tt.code, tt.err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// TestRedeemAction_DistinctValues compares all three action constants pairwise
// to protect their separate observable values. A caller can distinguish
// creation, redemption and completion skipping in the asserted enumeration.
func TestRedeemAction_DistinctValues(t *testing.T) {
	t.Parallel()
	assert.NotEqual(t, payment.RedeemActionCreate, payment.RedeemActionRedeem)
	assert.NotEqual(t, payment.RedeemActionCreate, payment.RedeemActionSkipCompleted)
	assert.NotEqual(t, payment.RedeemActionRedeem, payment.RedeemActionSkipCompleted)
}

// TestResolveRedeemAction_IsUsedCanUseConsistency supplies fully consumed used
// and available unused codes. It checks IsUsed and CanUse alongside the
// selected skip or redeem actions for those two fixture states.
func TestResolveRedeemAction_IsUsedCanUseConsistency(t *testing.T) {
	t.Parallel()

	usedCode := &billing.RedeemCode{Status: billing.StatusUsed, MaxUses: 1, UsedCount: 1}
	unusedCode := &billing.RedeemCode{Status: billing.StatusUnused, MaxUses: 1}

	assert.True(t, usedCode.IsUsed())
	assert.False(t, usedCode.CanUse())
	assert.Equal(t, payment.RedeemActionSkipCompleted, payment.ResolveRedeemAction(usedCode, nil))

	assert.False(t, unusedCode.IsUsed())
	assert.True(t, unusedCode.CanUse())
	assert.Equal(t, payment.RedeemActionRedeem, payment.ResolveRedeemAction(unusedCode, nil))
}

// TestExpectedNotificationProviderKeyPrefersOrderInstanceProvider registers
// official Alipay but supplies an EasyPay instance binding for the order. It
// expects that binding to determine callback provider identity for the visible
// Alipay method.
func TestExpectedNotificationProviderKeyPrefersOrderInstanceProvider(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymenttestkit.StaticProvider{
		Key:   payment.TypeAlipay,
		Types: []payment.PaymentType{payment.TypeAlipay},
	})

	assert.Equal(t,
		payment.TypeEasyPay,
		payment.ExpectedNotificationProviderKey(registry, payment.TypeAlipay, "", payment.TypeEasyPay),
	)
}

// TestExpectedNotificationProviderKeyUsesRegistryMappingForLegacyOrders
// supplies no saved provider identity and registers EasyPay for visible
// Alipay. It expects the legacy callback identity to follow that registry
// mapping.
func TestExpectedNotificationProviderKeyUsesRegistryMappingForLegacyOrders(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymenttestkit.StaticProvider{
		Key:   payment.TypeEasyPay,
		Types: []payment.PaymentType{payment.TypeAlipay},
	})

	assert.Equal(t,
		payment.TypeEasyPay,
		payment.ExpectedNotificationProviderKey(registry, payment.TypeAlipay, "", ""),
	)
}

// TestExpectedNotificationProviderKeyFallsBackToPaymentType supplies no
// registry or saved provider identity for a WeChat order. It expects the
// visible payment type to become the callback provider key.
func TestExpectedNotificationProviderKeyFallsBackToPaymentType(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		payment.TypeWxpay,
		payment.ExpectedNotificationProviderKey(nil, payment.TypeWxpay, "", ""),
	)
}

// TestExpectedNotificationProviderKeyPrefersOrderSnapshotProviderKey registers
// official Alipay and supplies EasyPay as the order-provider key argument. It
// requires that supplied key to win when the instance key is absent; this
// helper call does not exercise extraction from an order snapshot.
func TestExpectedNotificationProviderKeyPrefersOrderSnapshotProviderKey(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymenttestkit.StaticProvider{
		Key:   payment.TypeAlipay,
		Types: []payment.PaymentType{payment.TypeAlipay},
	})

	assert.Equal(t,
		payment.TypeEasyPay,
		payment.ExpectedNotificationProviderKey(registry, payment.TypeAlipay, payment.TypeEasyPay, ""),
	)
}

// TestValidateProviderNotificationMetadataRejectsWxpaySnapshotMismatch
// supplies a saved WeChat app identity and a different reported appid. It
// expects the app mismatch diagnostic while merchant and currency fixture
// values match.
func TestValidateProviderNotificationMetadataRejectsWxpaySnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &payment.Order{
		PaymentType: payment.TypeWxpay,
		ProviderSnapshot: map[string]any{
			"schema_version":  1,
			"merchant_app_id": "wx-app-expected",
			"merchant_id":     "mch-expected",
			"currency":        "CNY",
		},
	}

	err := payment.ValidateProviderNotificationMetadata(order, payment.TypeWxpay, map[string]string{
		"appid":       "wx-app-other",
		"mchid":       "mch-expected",
		"currency":    "CNY",
		"trade_state": "SUCCESS",
	})
	assert.ErrorContains(t, err, "wxpay appid mismatch")
}

// TestValidateProviderNotificationMetadataAllowsLegacyOrdersWithoutSnapshotFields
// supplies a legacy WeChat snapshot containing provider labels but no merchant
// identity. It accepts reported app, merchant and currency values for that
// incomplete historical snapshot.
func TestValidateProviderNotificationMetadataAllowsLegacyOrdersWithoutSnapshotFields(t *testing.T) {
	t.Parallel()

	order := &payment.Order{
		PaymentType: payment.TypeWxpay,
		ProviderSnapshot: map[string]any{
			"schema_version":       1,
			"provider_instance_id": "9",
			"provider_key":         payment.TypeWxpay,
		},
	}

	err := payment.ValidateProviderNotificationMetadata(order, payment.TypeWxpay, map[string]string{
		"appid":       "wx-app-runtime",
		"mchid":       "mch-runtime",
		"currency":    "CNY",
		"trade_state": "SUCCESS",
	})
	assert.NoError(t, err)
}

// TestIsValidProviderAmount compares positive money against zero, negative,
// NaN and positive infinity. It accepts the positive finite fixture and
// rejects the other listed observations before core paid confirmation.
func TestIsValidProviderAmount(t *testing.T) {
	t.Parallel()

	assert.True(t, payment.IsValidProviderAmount(0.01))
	assert.False(t, payment.IsValidProviderAmount(0))
	assert.False(t, payment.IsValidProviderAmount(-1))
	assert.False(t, payment.IsValidProviderAmount(math.NaN()))
	assert.False(t, payment.IsValidProviderAmount(math.Inf(1)))
}

// TestValidateProviderNotificationMetadataRejectsAlipaySnapshotMismatch
// supplies a version-two Alipay snapshot and different reported app_id. It
// expects an identity mismatch diagnostic from validation of the saved
// merchant app.
func TestValidateProviderNotificationMetadataRejectsAlipaySnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &payment.Order{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version":  2,
			"merchant_app_id": "alipay-app-expected",
		},
	}

	err := payment.ValidateProviderNotificationMetadata(order, payment.TypeAlipay, map[string]string{
		"app_id": "alipay-app-other",
	})
	assert.ErrorContains(t, err, "alipay app_id mismatch")
}

// TestValidateProviderNotificationMetadataRejectsEasyPaySnapshotMismatch
// models visible Alipay routed through EasyPay with a saved pid. It rejects a
// different reported merchant pid using the provider-specific validation rule.
func TestValidateProviderNotificationMetadataRejectsEasyPaySnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &payment.Order{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version": 2,
			"merchant_id":    "pid-expected",
		},
	}

	err := payment.ValidateProviderNotificationMetadata(order, payment.TypeEasyPay, map[string]string{
		"pid": "pid-other",
	})
	assert.ErrorContains(t, err, "easypay pid mismatch")
}

// TestValidateProviderNotificationMetadataRejectsAirwallexSnapshotMismatch
// first changes the reported account, then changes currency with the account
// matching. It expects the corresponding account and currency diagnostics for
// both observations.
func TestValidateProviderNotificationMetadataRejectsAirwallexSnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &payment.Order{
		PaymentType: payment.TypeAirwallex,
		ProviderSnapshot: map[string]any{
			"schema_version": 2,
			"merchant_id":    "acct_expected",
			"currency":       "CNY",
		},
	}

	err := payment.ValidateProviderNotificationMetadata(order, payment.TypeAirwallex, map[string]string{
		"account_id": "acct_other",
		"currency":   "CNY",
		"status":     "SUCCEEDED",
	})
	assert.ErrorContains(t, err, "airwallex account_id mismatch")

	err = payment.ValidateProviderNotificationMetadata(order, payment.TypeAirwallex, map[string]string{
		"account_id": "acct_expected",
		"currency":   "USD",
		"status":     "SUCCEEDED",
	})
	assert.ErrorContains(t, err, "airwallex currency mismatch")
}

// TestValidateProviderNotificationMetadataRejectsStripeCurrencyMismatch
// supplies saved HKD currency and a reported USD payment observation. It
// expects the Stripe currency mismatch diagnostic before that observation can
// be accepted locally.
func TestValidateProviderNotificationMetadataRejectsStripeCurrencyMismatch(t *testing.T) {
	t.Parallel()

	order := &payment.Order{
		PaymentType: payment.TypeStripe,
		ProviderSnapshot: map[string]any{
			"schema_version": 2,
			"currency":       "HKD",
		},
	}

	err := payment.ValidateProviderNotificationMetadata(order, payment.TypeStripe, map[string]string{
		"currency": "USD",
	})
	assert.ErrorContains(t, err, "stripe currency mismatch")
}

// TestAffiliateRebateBasePointsUsesConfiguredReasoningPointBase compares
// credited balance with subscription monthly, weekly and daily quota
// observations. It asserts quota precedence, invalid-quota skipping and zero
// base for missing quotas or order.
func TestAffiliateRebateBasePointsUsesConfiguredReasoningPointBase(t *testing.T) {
	t.Parallel()
	monthlyPoints := 1000.0
	weeklyPoints := 300.0
	dailyPoints := 100.0
	zeroPoints := 0.0

	tests := []struct {
		name  string         // named rebate base case
		order *payment.Order // supplied immutable order observation
		want  float64        // expected base in points
	}{
		{
			name: "余额订单使用实际到账积分",
			order: &payment.Order{
				OrderType: payment.OrderTypeBalance,
				Amount:    1000,
				PayAmount: 100,
			},
			want: 1000,
		},
		{
			name: "订阅订单优先使用月度积分额度",
			order: &payment.Order{
				OrderType: payment.OrderTypeSubscription,
				Amount:    100,
				PayAmount: 100,
				PlanSnapshot: billing.SubscriptionPlanSnapshot{
					MonthlyLimitUSD: &monthlyPoints,
					WeeklyLimitUSD:  &weeklyPoints,
					DailyLimitUSD:   &dailyPoints,
				},
			},
			want: 1000,
		},
		{
			name: "没有月额度时使用周额度",
			order: &payment.Order{
				OrderType: payment.OrderTypeSubscription,
				Amount:    100,
				PayAmount: 100,
				PlanSnapshot: billing.SubscriptionPlanSnapshot{
					WeeklyLimitUSD: &weeklyPoints,
					DailyLimitUSD:  &dailyPoints,
				},
			},
			want: 300,
		},
		{
			name: "没有月周额度时使用日额度",
			order: &payment.Order{
				OrderType: payment.OrderTypeSubscription,
				Amount:    100,
				PayAmount: 100,
				PlanSnapshot: billing.SubscriptionPlanSnapshot{
					DailyLimitUSD: &dailyPoints,
				},
			},
			want: 100,
		},
		{
			name: "跳过无效月额度后使用周额度",
			order: &payment.Order{
				OrderType: payment.OrderTypeSubscription,
				Amount:    100,
				PayAmount: 100,
				PlanSnapshot: billing.SubscriptionPlanSnapshot{
					MonthlyLimitUSD: &zeroPoints,
					WeeklyLimitUSD:  &weeklyPoints,
				},
			},
			want: 300,
		},
		{
			name: "没有额度时不使用套餐价格兜底",
			order: &payment.Order{
				OrderType: payment.OrderTypeSubscription,
				Amount:    100,
				PayAmount: 100,
			},
		},
		{name: "空订单不产生返利基数"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, payment.AffiliateRebateBasePoints(tt.order))
		})
	}
}
