/*
FILE: margin/types/account.go

DESCRIPTION:
Account / assets / borrow-repay / records domain types for the Bitget V2
MARGIN profile. Fields map straight from the V2 wire (verified against
the live docs and the cross-checked third-party type definitions).

CROSSED vs ISOLATED:

Several responses differ by mode. Rather than split the method surface,
the SDK returns ONE struct per concept carrying both the crossed
(single-coin) and isolated (base/quote, per-symbol) fields; only the
fields the venue sent for the pinned mode are populated. Each field
documents which mode produces it.
*/

package types

import "github.com/shopspring/decimal"

// MarginAsset — one row of GET /api/v2/margin/<mode>/account/assets.
// On crossed margin Symbol is empty (assets are per-coin); on isolated
// margin Symbol identifies the trading pair the balance belongs to.
type MarginAsset struct {
	Coin        string
	Symbol      string // isolated only
	TotalAmount decimal.Decimal
	Available   decimal.Decimal
	Frozen      decimal.Decimal
	Borrow      decimal.Decimal
	Interest    decimal.Decimal
	Net         decimal.Decimal
	Coupon      decimal.Decimal
	UpdatedAtMs int64
}

// AccountUpdate — one per-coin (crossed) or per-symbol-coin (isolated)
// row pushed by the private "account-<mode>" WS channel. Mirrors the
// REST MarginAsset shape but carries only the fields the channel ships
// (no TotalAmount / Net — those are REST-only aggregates).
type AccountUpdate struct {
	Coin        string
	Symbol      string // isolated only
	Available   decimal.Decimal
	Frozen      decimal.Decimal
	Borrow      decimal.Decimal
	Interest    decimal.Decimal
	Coupon      decimal.Decimal
	UpdatedAtMs int64
}

// BorrowResult — response of POST account/borrow.
type BorrowResult struct {
	LoanID       string
	Coin         string
	Symbol       string // isolated only
	BorrowAmount decimal.Decimal
}

// RepayResult — response of POST account/repay.
type RepayResult struct {
	RepayID          string
	Coin             string
	Symbol           string // isolated only
	RepayAmount      decimal.Decimal
	RemainDebtAmount decimal.Decimal
}

// MaxBorrowable — response of GET account/max-borrowable-amount.
// Crossed populates Coin + MaxBorrowableAmount; isolated populates
// Symbol + Base/Quote variants.
type MaxBorrowable struct {
	// crossed
	Coin                string
	MaxBorrowableAmount decimal.Decimal
	// isolated
	Symbol                   string
	BaseCoin                 string
	BaseCoinMaxBorrowAmount  decimal.Decimal
	QuoteCoin                string
	QuoteCoinMaxBorrowAmount decimal.Decimal
}

// MaxTransferOut — response of GET account/max-transfer-out-amount.
// Crossed populates Coin + MaxTransferOutAmount; isolated populates
// Symbol + Base/Quote variants.
type MaxTransferOut struct {
	// crossed
	Coin                 string
	MaxTransferOutAmount decimal.Decimal
	// isolated
	Symbol                        string
	BaseCoin                      string
	BaseCoinMaxTransferOutAmount  decimal.Decimal
	QuoteCoin                     string
	QuoteCoinMaxTransferOutAmount decimal.Decimal
}

// Currency — one row of GET /api/v2/margin/currencies (mode-agnostic
// reference data: which coins / pairs are marginable and at what limits).
type Currency struct {
	Symbol              string
	BaseCoin            string
	QuoteCoin           string
	MaxCrossedLeverage  string
	MaxIsolatedLeverage string
	MinTradeAmount      decimal.Decimal
	MaxTradeAmount      decimal.Decimal
	MinTradeUSDT        decimal.Decimal
	TakerFeeRate        decimal.Decimal
	MakerFeeRate        decimal.Decimal
	PricePrecision      int
	QuantityPrecision   int
	UserMinBorrow       decimal.Decimal
	Status              string
	// Borrowability flags. IsBorrowable is the crossed/legacy flag;
	// the isolated base/quote flags are per-side.
	IsBorrowable              bool
	IsCrossBorrowable         bool
	IsIsolatedBaseBorrowable  bool
	IsIsolatedQuoteBorrowable bool
}

// Fill — one trade execution from GET account fills.
type Fill struct {
	OrderID     string
	TradeID     string
	Side        string
	OrderType   string
	FillPrice   decimal.Decimal
	Size        decimal.Decimal
	Amount      decimal.Decimal
	TotalFee    decimal.Decimal
	FeeCoin     string
	TradeScope  string
	CreatedAtMs int64
}

// BorrowRecord — one row of GET borrow-history.
type BorrowRecord struct {
	LoanID       string
	Coin         string
	BorrowAmount decimal.Decimal
	BorrowType   string
	CreatedAtMs  int64
	UpdatedAtMs  int64
}

// RepayRecord — one row of GET repay-history.
type RepayRecord struct {
	RepayID        string
	Coin           string
	Symbol         string
	RepayAmount    decimal.Decimal
	RepayInterest  decimal.Decimal
	RepayPrincipal decimal.Decimal
	RepayType      string
	CreatedAtMs    int64
	UpdatedAtMs    int64
}

// InterestRecord — one row of GET interest-history.
type InterestRecord struct {
	InterestID        string
	InterestCoin      string
	LoanCoin          string
	Symbol            string
	DailyInterestRate decimal.Decimal
	InterestAmount    decimal.Decimal
	InterestType      string
	CreatedAtMs       int64
	UpdatedAtMs       int64
}

// LiquidationRecord — one row of GET liquidation-history.
type LiquidationRecord struct {
	LiqID          string
	Symbol         string
	LiqStartTimeMs int64
	LiqEndTimeMs   int64
	LiqRiskRatio   decimal.Decimal
	TotalAssets    decimal.Decimal
	TotalDebt      decimal.Decimal
	LiqFee         decimal.Decimal
	CreatedAtMs    int64
	UpdatedAtMs    int64
}

// FinancialRecord — one row of GET financial-records.
type FinancialRecord struct {
	Coin        string
	Symbol      string
	MarginID    string
	MarginType  string
	Amount      decimal.Decimal
	Balance     decimal.Decimal
	Fee         decimal.Decimal
	CreatedAtMs int64
	UpdatedAtMs int64
}
