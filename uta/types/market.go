/*
FILE: uta/types/market.go

DESCRIPTION:
Domain types for the V3 UTA public market-data EXTRAS: fee groups, score
weights, proof-of-reserves, open interest, funding rates, risk reserve,
discount rate, margin loans, position tiers, OI limits and index
components. Field mapping verified against the Bitget V3 docs and the
tiagosiebler reference types. decimal for numeric fields, int64 epoch-ms
for timestamps.
*/

package types

import "github.com/shopspring/decimal"

// FeeGroupLabel — one symbol-grouping label of a market-maker fee group.
type FeeGroupLabel struct {
	Label   string
	Weight  decimal.Decimal
	Symbols []string
}

// FeeGroupTier — one maker-fee tier of a market-maker fee group.
type FeeGroupTier struct {
	Level        string
	MakerFeeRate decimal.Decimal
}

// MarketFeeGroup — GET market/fee-group.
type MarketFeeGroup struct {
	Category string
	Group    string
	Labels   []FeeGroupLabel
	Tiers    []FeeGroupTier
}

// ScoreWeight — one row of GET market/score-weights.
type ScoreWeight struct {
	Category       string
	Label          string
	Symbol         string
	RequiredSpread decimal.Decimal
	MinMakerVolume decimal.Decimal
	Weight         decimal.Decimal
}

// ProofOfReservesItem — one coin's reserve breakdown.
type ProofOfReservesItem struct {
	Coin           string
	UserAssets     decimal.Decimal
	PlatformAssets decimal.Decimal
	ReserveRatio   decimal.Decimal
}

// ProofOfReserves — GET market/proof-of-reserves.
type ProofOfReserves struct {
	MerkleRootHash    string
	TotalReserveRatio decimal.Decimal
	List              []ProofOfReservesItem
}

// OpenInterestItem — one symbol's open interest.
type OpenInterestItem struct {
	Symbol       string
	OpenInterest decimal.Decimal
}

// OpenInterest — GET market/open-interest.
type OpenInterest struct {
	TimeMs int64
	List   []OpenInterestItem
}

// CurrentFundingRate — GET market/current-fund-rate.
type CurrentFundingRate struct {
	Symbol              string
	FundingRate         decimal.Decimal
	FundingRateInterval string
	NextUpdateMs        int64
	MinFundingRate      decimal.Decimal
	MaxFundingRate      decimal.Decimal
}

// HistoryFundingRate — one row of GET market/history-fund-rate.
type HistoryFundingRate struct {
	Symbol      string
	FundingRate decimal.Decimal
	TimeMs      int64
}

// RiskReserveRecord — one balance snapshot in a risk-reserve series.
type RiskReserveRecord struct {
	Balance decimal.Decimal
	Amount  decimal.Decimal
	TimeMs  int64
	Type    string
}

// RiskReserve — GET market/risk-reserve | risk-reserve-hour.
type RiskReserve struct {
	Coin         string
	TotalBalance decimal.Decimal
	Records      []RiskReserveRecord
}

// RiskReserveAllItem — one coin's aggregate insurance fund.
type RiskReserveAllItem struct {
	Coin    string
	Balance decimal.Decimal
	Symbols []string
}

// DiscountRateTier — one tier of a coin's collateral discount curve.
type DiscountRateTier struct {
	TierStartValue decimal.Decimal
	DiscountRate   decimal.Decimal
}

// DiscountRate — one coin's discount-rate curve (GET market/discount-rate).
type DiscountRate struct {
	Coin string
	List []DiscountRateTier
}

// MarginLoan — GET market/margin-loans.
type MarginLoan struct {
	DailyInterest  decimal.Decimal
	AnnualInterest decimal.Decimal
	Limit          decimal.Decimal
}

// PositionTier — one row of GET market/position-tier.
type PositionTier struct {
	Tier         string
	MinTierValue decimal.Decimal
	MaxTierValue decimal.Decimal
	Leverage     decimal.Decimal
	MMR          decimal.Decimal
}

// ContractOi — one row of GET market/oi-limit (open-interest limits).
type ContractOi struct {
	Symbol             string
	NotionalValue      decimal.Decimal
	TotalNotionalValue decimal.Decimal
}

// IndexComponent — one constituent of an index price.
type IndexComponent struct {
	Exchange        string
	SpotPair        string
	EquivalentPrice decimal.Decimal
	Weight          decimal.Decimal
}

// IndexPriceComponents — GET market/index-components.
type IndexPriceComponents struct {
	Symbol     string
	Components []IndexComponent
}
