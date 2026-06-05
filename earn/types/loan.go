/*
FILE: earn/types/loan.go

DESCRIPTION:
Domain types for the EARN Crypto Loan category. Field mapping verified
against the Bitget V2 earn/loan reference request/response types
(EarnLoanCurrenciesV2 / EarnLoanOrdersV2 / EarnLoanRepayResponseV2 /
EarnLoanRepayHistoryV2 / EarnLoanPledgeRateHistoryV2 / EarnLoanHistoryV2 /
EarnLoanDebtsV2 / EarnLoanLiquidationRecordsV2). *Time fields are Unix-ms.
*/

package types

import "github.com/shopspring/decimal"

// LoanCurrencyLoanInfo — one borrowable-coin row of GET loan/public/coinInfos.
type LoanCurrencyLoanInfo struct {
	Coin        string
	HourRate7D  decimal.Decimal
	Rate7D      decimal.Decimal
	HourRate30D decimal.Decimal
	Rate30D     decimal.Decimal
	MinUsdt     decimal.Decimal
	MaxUsdt     decimal.Decimal
	Min         decimal.Decimal
	Max         decimal.Decimal
}

// LoanCurrencyPledgeInfo — one collateral-coin row of GET loan/public/coinInfos.
type LoanCurrencyPledgeInfo struct {
	Coin      string
	InitRate  decimal.Decimal
	SupRate   decimal.Decimal
	ForceRate decimal.Decimal
	MinUsdt   decimal.Decimal
	MaxUsdt   decimal.Decimal
}

// LoanCurrencies — response of GET loan/public/coinInfos.
type LoanCurrencies struct {
	LoanInfos   []LoanCurrencyLoanInfo
	PledgeInfos []LoanCurrencyPledgeInfo
}

// LoanEstInterest — response of GET loan/public/hour-interest: estimated
// hourly interest and the borrowable loan amount for the given inputs.
type LoanEstInterest struct {
	HourInterest decimal.Decimal
	LoanAmount   decimal.Decimal
}

// LoanBorrowRequest — input to POST loan/borrow. LoanCoin, PledgeCoin and
// Daily ("SEVEN" | "THIRTY") are required; provide exactly one of
// PledgeAmount / LoanAmount.
type LoanBorrowRequest struct {
	LoanCoin     string
	PledgeCoin   string
	Daily        string
	PledgeAmount string
	LoanAmount   string
}

// LoanOrder — one row of GET loan/ongoing-orders.
type LoanOrder struct {
	OrderID          string
	LoanCoin         string
	PledgeCoin       string
	LoanAmount       decimal.Decimal
	InterestAmount   decimal.Decimal
	HourInterestRate decimal.Decimal
	PledgeAmount     decimal.Decimal
	PledgeRate       decimal.Decimal
	SupRate          decimal.Decimal
	ForceRate        decimal.Decimal
	BorrowTimeMs     int64
	ExpireTimeMs     int64
}

// LoanRepayRequest — input to POST loan/repay. OrderID and RepayAll
// ("true" | "false") are required; Amount / RepayUnlock are optional.
type LoanRepayRequest struct {
	OrderID     string
	Amount      string
	RepayUnlock string
	RepayAll    string
}

// LoanRepayResult — response of POST loan/repay.
type LoanRepayResult struct {
	LoanCoin          string
	PledgeCoin        string
	RepayAmount       decimal.Decimal
	PayInterest       decimal.Decimal
	RepayLoanAmount   decimal.Decimal
	RepayUnlockAmount decimal.Decimal
}

// LoanRepayHistory — one row of GET loan/repay-history.
type LoanRepayHistory struct {
	OrderID           string
	LoanCoin          string
	PledgeCoin        string
	RepayAmount       decimal.Decimal
	PayInterest       decimal.Decimal
	RepayLoanAmount   decimal.Decimal
	RepayUnlockAmount decimal.Decimal
	RepayTimeMs       int64
}

// LoanRevisePledgeRequest — input to POST loan/revise-pledge. All fields
// required. ReviseType selects add/remove collateral (venue values).
type LoanRevisePledgeRequest struct {
	OrderID    string
	Amount     string
	PledgeCoin string
	ReviseType string
}

// LoanRevisePledgeResult — response of POST loan/revise-pledge.
type LoanRevisePledgeResult struct {
	LoanCoin        string
	PledgeCoin      string
	AfterPledgeRate decimal.Decimal
}

// LoanPledgeRateHistory — one row of GET loan/revise-history.
type LoanPledgeRateHistory struct {
	OrderID          string
	LoanCoin         string
	PledgeCoin       string
	ReviseSide       string
	ReviseAmount     decimal.Decimal
	AfterPledgeRate  decimal.Decimal
	BeforePledgeRate decimal.Decimal
	ReviseTimeMs     int64
}

// LoanHistory — one row of GET loan/borrow-history.
type LoanHistory struct {
	OrderID          string
	LoanCoin         string
	PledgeCoin       string
	Status           string
	Daily            string
	InitPledgeAmount decimal.Decimal
	InitLoanAmount   decimal.Decimal
	HourRate         decimal.Decimal
	BorrowTimeMs     int64
}

// LoanDebtInfo — one coin row of GET loan/debts (used for both the
// pledge side and the loan side).
type LoanDebtInfo struct {
	Coin       string
	Amount     decimal.Decimal
	AmountUsdt decimal.Decimal
}

// LoanDebts — response of GET loan/debts.
type LoanDebts struct {
	PledgeInfos []LoanDebtInfo
	LoanInfos   []LoanDebtInfo
}

// LoanLiquidationRecord — one row of GET loan/reduces (collateral
// liquidations).
type LoanLiquidationRecord struct {
	OrderID         string
	LoanCoin        string
	PledgeCoin      string
	Status          string
	PledgeRate      decimal.Decimal
	PledgePrice     decimal.Decimal
	PledgeAmount    decimal.Decimal
	ReduceFee       decimal.Decimal
	ResidueAmount   decimal.Decimal
	RunlockAmount   decimal.Decimal
	RepayLoanAmount decimal.Decimal
	ReduceTimeMs    int64
}
