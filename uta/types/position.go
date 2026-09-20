/*
FILE: uta/types/position.go

DESCRIPTION:
Domain types for the V3 UTA positions surface: current positions, position
history, max-open-available and ADL rank. Field mapping verified against
the Bitget V3 docs and the tiagosiebler reference types (CurrentPositionV3
/ PositionHistoryV3 / GetMaxOpenAvailableResponseV3 / PositionAdlRankV3).
decimal for monetary fields, int64 epoch-ms.
*/

package types

import "github.com/shopspring/decimal"

// CurrentPosition — one open position (position/current-position, and the
// rows of the private WS `position` topic via
// uta.StreamClient.WatchPositions).
//
// WS NOTES:
//   - The WS row carries NO category per the venue docs — Category stays
//     empty on WS rows (it is filled, upper-cased, only if the venue ever
//     sends it). Match WS positions by Symbol (+ PosSide in hedge mode).
//   - Total carries the WS `size`; LiquidationPrice the WS `liqPrice`;
//     TotalFunding the WS `totalFundingFee`.
//   - A closed position arrives as a row with Total = 0 and
//     PositionStatus = "ended".
type CurrentPosition struct {
	Category         string
	Symbol           string
	MarginCoin       string
	HoldMode         string
	PosSide          string
	MarginMode       string
	PositionBalance  decimal.Decimal
	Available        decimal.Decimal
	Frozen           decimal.Decimal
	Total            decimal.Decimal
	Leverage         decimal.Decimal
	CurRealisedPnl   decimal.Decimal
	AvgPrice         decimal.Decimal
	PositionStatus   string
	UnrealisedPnl    decimal.Decimal
	LiquidationPrice decimal.Decimal
	MMR              decimal.Decimal
	ProfitRate       decimal.Decimal
	MarkPrice        decimal.Decimal
	BreakEvenPrice   decimal.Decimal
	TotalFunding     decimal.Decimal
	OpenFeeTotal     decimal.Decimal
	CloseFeeTotal    decimal.Decimal
	CreatedTime      int64
	UpdatedTime      int64

	// MarginSize — WS-only: margin allocated to the position (WS
	// `marginSize`). Zero on REST rows (REST reports PositionBalance).
	MarginSize decimal.Decimal
}

// PositionHistory — one closed position (position/history-position).
type PositionHistory struct {
	PositionID     string
	Category       string
	Symbol         string
	MarginCoin     string
	HoldMode       string
	PosSide        string
	MarginMode     string
	OpenPriceAvg   decimal.Decimal
	ClosePriceAvg  decimal.Decimal
	OpenTotalPos   decimal.Decimal
	CloseTotalPos  decimal.Decimal
	CumRealisedPnl decimal.Decimal
	NetProfit      decimal.Decimal
	TotalFunding   decimal.Decimal
	OpenFeeTotal   decimal.Decimal
	CloseFeeTotal  decimal.Decimal
	CreatedTime    int64
	UpdatedTime    int64
}

// MaxOpenAvailable — account/max-open-available: how much can still be
// opened given the prospective order.
type MaxOpenAvailable struct {
	Available    decimal.Decimal
	MaxOpen      decimal.Decimal
	BuyOpenCost  decimal.Decimal
	SellOpenCost decimal.Decimal
	MaxBuyOpen   decimal.Decimal
	MaxSellOpen  decimal.Decimal
}

// PositionAdlRank — one row of position/adlRank (auto-deleveraging queue).
type PositionAdlRank struct {
	Symbol     string
	MarginCoin string
	AdlRank    string
	HoldSide   string
}
