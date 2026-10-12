package billing

import (
	"context"

	"github.com/TokenFlux/TokenRouter/internal/pkg/apperror"
)

var ErrBalanceNegative = apperror.BadRequest("BALANCE_NEGATIVE", "balance cannot be negative")

// BalanceChange 记录一次余额变更前后的值。
type BalanceChange struct {
	Old float64
	New float64
}

// BalanceAdjuster 提供闭合原子调账，不接受完整用户快照。
type BalanceAdjuster interface {
	SetBalance(context.Context, int64, float64) (BalanceChange, error)
	AdjustBalance(context.Context, int64, float64) (BalanceChange, error)
}

// InitialUserFunds 是注册时赠送的余额，累计充值额保持不变。
type InitialUserFunds struct{ Balance float64 }
