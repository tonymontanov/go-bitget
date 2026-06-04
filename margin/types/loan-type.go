/*
FILE: margin/types/loan-type.go

DESCRIPTION:
Margin-only enums on the Bitget V2 order wire: LoanType (auto-borrow /
auto-repay behaviour) and STPMode (self-trade prevention). Both are
lower / camel-case strings exactly as Bitget accepts them — keep the
values byte-exact or the venue rejects with code=40034.
*/

package types

// LoanType controls the auto-borrow / auto-repay behaviour of a margin
// order. Bitget marks the field REQUIRED on every place / batch-place
// request; the SDK defaults an empty value to LoanTypeNormal so the
// wire field is always populated.
type LoanType string

const (
	// LoanTypeNormal — place a plain order against existing collateral
	// (no auto-borrow, no auto-repay). SDK default.
	LoanTypeNormal LoanType = "normal"
	// LoanTypeAutoLoan — auto-borrow the shortfall to fund the order.
	LoanTypeAutoLoan LoanType = "autoLoan"
	// LoanTypeAutoRepay — auto-repay outstanding debt with the proceeds
	// on fill.
	LoanTypeAutoRepay LoanType = "autoRepay"
	// LoanTypeAutoLoanAndRepay — auto-borrow to fund AND auto-repay on
	// fill.
	LoanTypeAutoLoanAndRepay LoanType = "autoLoanAndRepay"
)

// STPMode — self-trade prevention policy applied when the order would
// match against another order from the same account. Empty means the
// venue default ("none").
type STPMode string

const (
	// STPModeNone — no self-trade prevention (venue default).
	STPModeNone STPMode = "none"
	// STPModeCancelTaker — cancel the incoming (taker) order.
	STPModeCancelTaker STPMode = "cancel_taker"
	// STPModeCancelMaker — cancel the resting (maker) order.
	STPModeCancelMaker STPMode = "cancel_maker"
	// STPModeCancelBoth — cancel both taker and maker orders.
	STPModeCancelBoth STPMode = "cancel_both"
)
