/*
FILE: earn/types/savings.go

DESCRIPTION:
Domain types for the EARN Savings category plus the shared Earn-account
overview. Field mapping verified against the Bitget V2 earn/savings docs
and the tiagosiebler reference types (EarnSavingsProductsV2 /
EarnSavingsAccountV2 / EarnSavingsAssetV2 / EarnSavingsRecordV2 /
EarnSavingsSubscriptionDetailV2).
*/

package types

import "github.com/shopspring/decimal"

// EarnAsset — one row of GET earn/account/assets (the Earn account
// overview): a coin and the amount held across all earn products.
type EarnAsset struct {
	Coin   string
	Amount decimal.Decimal
}

// SavingsAPYTier — one ladder rate row of a savings product
// (apyList). For single-rate products there is one tier.
type SavingsAPYTier struct {
	RateLevel  string
	MinStepVal decimal.Decimal
	MaxStepVal decimal.Decimal
	CurrentApy decimal.Decimal
}

// SavingsProduct — one row of GET earn/savings/product.
//
//	PeriodType: "flexible" | "fixed"
//	ApyType:    "single" | "ladder"
//	SettleMethod: "daily" | "maturity"
//	Status:     not_started | in_progress | paused | completed | sold_out | off_line
type SavingsProduct struct {
	ProductID     string
	Coin          string
	PeriodType    string
	Period        string
	ApyType       string
	AdvanceRedeem string // "Yes" / "No"
	SettleMethod  string
	Status        string
	ProductLevel  string
	ApyList       []SavingsAPYTier
}

// SavingsAccount — response of GET earn/savings/account: the BTC/USDT
// holdings + earnings overview.
type SavingsAccount struct {
	BTCAmount        decimal.Decimal
	USDTAmount       decimal.Decimal
	BTC24hEarning    decimal.Decimal
	USDT24hEarning   decimal.Decimal
	BTCTotalEarning  decimal.Decimal
	USDTTotalEarning decimal.Decimal
}

// SavingsAssetAPY — one ladder rate row of a held savings asset.
type SavingsAssetAPY struct {
	RateLevel  string
	MinApy     decimal.Decimal
	MaxApy     decimal.Decimal
	CurrentApy decimal.Decimal
}

// SavingsAsset — one held savings position (GET earn/savings/assets).
type SavingsAsset struct {
	ProductID       string
	OrderID         string
	ProductCoin     string
	InterestCoin    string
	PeriodType      string
	Period          string
	Status          string
	AllowRedemption string // "Yes" / "No"
	ProductLevel    string

	HoldAmount  decimal.Decimal
	LastProfit  decimal.Decimal
	TotalProfit decimal.Decimal
	HoldDays    int64
	Apy         []SavingsAssetAPY
}

// SavingsRecord — one row of GET earn/savings/records (subscribe /
// redeem / interest history).
type SavingsRecord struct {
	OrderID        string
	CoinName       string
	SettleCoinName string
	ProductType    string
	Period         string
	ProductLevel   string
	OrderType      string
	Amount         decimal.Decimal
	TimeMs         int64
}

// SavingsSubscribeInfo — response of GET earn/savings/subscribe-info.
// Amounts are decimals; precisions / the various *Time markers and
// redeemDelay are kept as raw strings (their exact units are not pinned
// by the docs).
type SavingsSubscribeInfo struct {
	SingleMinAmount    decimal.Decimal
	SingleMaxAmount    decimal.Decimal
	RemainingAmount    decimal.Decimal
	SubscribePrecision string
	ProfitPrecision    string
	SubscribeTime      string
	InterestTime       string
	SettleTime         string
	ExpireTime         string
	RedeemTime         string
	SettleMethod       string
	RedeemDelay        string
	ApyList            []SavingsAPYTier
}

// SavingsRedeemRequest — input to POST earn/savings/redeem. ProductID,
// PeriodType and Amount are required; OrderID is optional (the held
// asset's order id, from GET assets).
type SavingsRedeemRequest struct {
	ProductID  string
	PeriodType string
	Amount     string
	OrderID    string
}

// RedeemResult — response of POST earn/savings/redeem.
type RedeemResult struct {
	OrderID string
	Status  string
}

// OpResult — response of the savings subscribe-result / redeem-result
// queries: a terminal result plus an optional failure message.
type OpResult struct {
	Result string // "success" / "fail"
	Msg    string
}
