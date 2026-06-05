/*
FILE: copytrading/types/trader.go

DESCRIPTION:
Domain types for the TRADER (lead) side of copy trading. M3a covers the
futures trader order surface; later milestones (M3b config, M3c profit)
add their own types here.

Field mapping verified against the Bitget V2 mix-trader docs
(order-current-track / order-history-track / order-total-detail /
order-modify-tpsl / order-close-positions).
*/

package types

import "github.com/shopspring/decimal"

// TraderCurrentOrder — one live lead position being copied
// (GET mix-trader/order-current-track). PresetStop* are the TP/SL the
// trader has set on the position; FollowCount is how many followers
// mirror this order.
type TraderCurrentOrder struct {
	TrackingNo  string
	OpenOrderID string
	Symbol      string
	PosSide     string

	OpenLeverage           decimal.Decimal
	OpenPriceAvg           decimal.Decimal
	OpenSize               decimal.Decimal
	PresetStopSurplusPrice decimal.Decimal
	PresetStopLossPrice    decimal.Decimal
	OpenFee                decimal.Decimal
	OpenTimeMs             int64
	FollowCount            int64
}

// TraderHistoryOrder — one closed lead position
// (GET mix-trader/order-history-track). StopType records whether the
// close was a take-profit or stop-loss.
type TraderHistoryOrder struct {
	TrackingNo   string
	Symbol       string
	OpenOrderID  string
	CloseOrderID string
	ProductType  string
	PosSide      string
	StopType     string // "profit" / "loss" / ""

	OpenLeverage  decimal.Decimal
	OpenPriceAvg  decimal.Decimal
	OpenSize      decimal.Decimal
	OpenFee       decimal.Decimal
	OpenTimeMs    int64
	ClosePriceAvg decimal.Decimal
	CloseSize     decimal.Decimal
	CloseFee      decimal.Decimal
	CloseTimeMs   int64
	AchievedPL    decimal.Decimal
	CTimeMs       int64
}

// RatePoint / ProfitPoint — one (rate|amount, time) sample in the
// trader summary's weekly / monthly series.
type RatePoint struct {
	Rate   decimal.Decimal
	TimeMs int64
}

type ProfitPoint struct {
	Amount decimal.Decimal
	TimeMs int64
}

// TraderOrderSummary — the lead trader's headline stats
// (GET mix-trader/order-total-detail).
//
// TotalPL is kept as the RAW venue string because Bitget returns it with
// a currency prefix (e.g. "$46.95"), which a decimal cannot represent.
// The numeric companions (ROI / WinRate / TotalEquity) parse cleanly.
type TraderOrderSummary struct {
	ROI         decimal.Decimal
	WinRate     decimal.Decimal
	TotalEquity decimal.Decimal
	TotalPL     string

	TradingOrderNum    int64
	TotalFollowerNum   int64
	CurrentFollowerNum int64
	GainNum            int64
	LossNum            int64

	TradingPairsAvailable []string
	LastWeekROI           []RatePoint
	LastMonthROI          []RatePoint
	LastWeekProfit        []ProfitPoint
	LastMonthProfit       []ProfitPoint
}

// TraderCloseRequest — input to the trader close-positions endpoint.
// Empty TrackingNo + empty Symbol closes EVERY position on the pinned
// product line. ProductType is taken from the client (pinned).
type TraderCloseRequest struct {
	TrackingNo string
	Symbol     string
}

// TraderClosedOrder — one row of the trader close-positions response:
// the tracking orders the close actually targeted.
type TraderClosedOrder struct {
	TrackingNo  string
	Symbol      string
	ProductType string
}

// TraderModifyTPSLRequest — input to mix-trader/order-modify-tpsl.
// TrackingNo is required and at least one of the prices must be set.
// Prices are strings to preserve Bitget's three-way semantics:
// empty = leave unchanged, "0" = cancel existing, > 0 = set/update.
// ProductType is taken from the client (pinned).
type TraderModifyTPSLRequest struct {
	TrackingNo       string
	Symbol           string
	StopSurplusPrice string
	StopLossPrice    string
}
