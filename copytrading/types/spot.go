/*
FILE: copytrading/types/spot.go

DESCRIPTION:
Domain types for SPOT copy trading. M4a covers the spot TRADER (lead)
surface; M4b adds the spot FOLLOWER types here.

Spot copy trading is NOT product-type scoped (it is spot), so none of
these carry a productType. Spot tracks buy/sell orders rather than
long/short positions.

Field mapping verified against the Bitget V2 spot-trader docs
(order-current-track / order-history-track / order-total-detail /
order-modify-tpsl / order-close-tracking / config-query-settings /
config-setting-symbols / config-query-followers / config-remove-follower
/ profit-summarys / profit-history-details / profit-details).
*/

package types

import "github.com/shopspring/decimal"

// SpotTraderCurrentOrder — one live lead spot tracking order
// (GET spot-trader/order-current-track). UnrealizedPLR is a percentage
// (11.11 == 11.11%).
type SpotTraderCurrentOrder struct {
	TrackingNo string
	OrderID    string
	Symbol     string

	BuyFillSize      decimal.Decimal
	BuyDelegateSize  decimal.Decimal
	BuyPrice         decimal.Decimal
	BuyFee           decimal.Decimal
	BuyTimeMs        int64
	UnrealizedPL     decimal.Decimal
	UnrealizedPLR    decimal.Decimal
	StopSurplusPrice decimal.Decimal
	StopLossPrice    decimal.Decimal
	FollowCount      int64
}

// SpotTraderHistoryOrder — one closed lead spot tracking order
// (GET spot-trader/order-history-track). AchievedPLR is a percentage.
type SpotTraderHistoryOrder struct {
	TrackingNo string
	Symbol     string

	FillSize    decimal.Decimal
	BuyPrice    decimal.Decimal
	SellPrice   decimal.Decimal
	BuyFee      decimal.Decimal
	SellFee     decimal.Decimal
	BuyTimeMs   int64
	SellTimeMs  int64
	AchievedPL  decimal.Decimal
	AchievedPLR decimal.Decimal
	NetProfit   decimal.Decimal
	FollowCount int64
}

// SpotTraderSummary — the spot lead trader's headline stats
// (GET spot-trader/order-total-detail). TotalPL is kept raw (string) for
// parity with the futures summary, which can carry a currency prefix.
// Reuses RatePoint / ProfitPoint (defined in trader.go).
type SpotTraderSummary struct {
	TotalFollowerNum   int64
	CurrentFollowerNum int64
	MaxFollowerNum     int64
	TradingOrderNum    int64
	GainNum            int64
	LossNum            int64

	TotalPL     string
	TotalEquity decimal.Decimal
	WinRate     decimal.Decimal

	LastWeekROI     []RatePoint
	LastMonthROI    []RatePoint
	LastWeekProfit  []ProfitPoint
	LastMonthProfit []ProfitPoint
}

// SpotQuoteInfo / SpotLabel / SpotTraceSymbol — sub-objects of the spot
// trader configuration.
type SpotQuoteInfo struct {
	Symbol           string
	MaxQuoteSize     decimal.Decimal
	SurplusQuoteSize decimal.Decimal
}

type SpotLabel struct {
	ID   string
	Name string
}

type SpotTraceSymbol struct {
	Symbol       string
	Enable       string // "YES"/"NO"
	MinOpenCount decimal.Decimal
}

// SpotTraderConfig — response of GET spot-trader/config-query-settings.
// Enable / ShowAssetsMap / ShowEquity are "YES"/"NO".
type SpotTraderConfig struct {
	RemoveLimitUsdt decimal.Decimal
	Enable          string
	ShowAssetsMap   string
	ShowEquity      string
	SpotInfoList    []SpotQuoteInfo
	Labels          []SpotLabel
	TraceSymbols    []SpotTraceSymbol
}

// SpotTraderModifyTPSLRequest — input to spot-trader/order-modify-tpsl.
// TrackingNo is required and at least one price must be set. Prices are
// strings to preserve the empty / "0" / >0 semantics. (Spot has no
// productType and no symbol on this call.)
type SpotTraderModifyTPSLRequest struct {
	TrackingNo       string
	StopSurplusPrice string
	StopLossPrice    string
}

// SpotProfitHistoryCoin — per-currency profit-share rollup inside
// SpotProfitSummary, with a by-date breakdown (ProfitPoint: amount=profit,
// time=profitTime).
type SpotProfitHistoryCoin struct {
	Coin             string
	ProfitCount      decimal.Decimal
	LastProfitTimeMs int64
	ByDate           []ProfitPoint
}

// SpotProfitSummary — response of GET spot-trader/profit-summarys.
type SpotProfitSummary struct {
	YesterdayProfit decimal.Decimal
	SumProfit       decimal.Decimal
	WaitProfit      decimal.Decimal
	YesterdayTimeMs int64
	History         []SpotProfitHistoryCoin
}

// SpotProfitShareRecord — one row of GET spot-trader/profit-history-details.
// DistributeRatio is a percentage.
type SpotProfitShareRecord struct {
	ProfitID        string
	Coin            string
	FollowerName    string
	DistributeRatio decimal.Decimal
	Profit          decimal.Decimal
	ProfitTimeMs    int64
}

// SpotPendingProfitShare — one row of GET spot-trader/profit-details
// (unrealized / to-be-distributed). DistributeRatio is a percentage.
type SpotPendingProfitShare struct {
	Coin            string
	FollowerName    string
	DistributeRatio decimal.Decimal
	Profit          decimal.Decimal
}

// =====================================================================
// M4b — SPOT FOLLOWER
// =====================================================================

// SpotMyTrader — one row of GET spot-follower/query-traders (the
// follower's "my traders" directory). Platsk* fields are membership-tier
// limits/counts that the venue may return empty.
type SpotMyTrader struct {
	TraderID          string
	TraderName        string
	CertificationType string // "Certified" / "Uncertified"

	TraceTotalAmount    decimal.Decimal
	TraceTotalNetProfit decimal.Decimal
	TraceTotalProfit    decimal.Decimal

	MaxFollowLimit       int64
	FollowCount          int64
	PlatskMaxFollowLimit int64
	PlatskFollowCount    int64

	FollowerTimeMs int64
}

// SpotFollowerCurrentOrder — one live copied spot order
// (GET spot-follower/query-current-orders). UnrealizedPLR is a percentage.
type SpotFollowerCurrentOrder struct {
	TrackingNo string
	TraderID   string
	Symbol     string

	BuyFillSize      decimal.Decimal
	BuyDelegateSize  decimal.Decimal
	BuyPrice         decimal.Decimal
	BuyFee           decimal.Decimal
	BuyTimeMs        int64
	UnrealizedPL     decimal.Decimal
	UnrealizedPLR    decimal.Decimal
	StopSurplusPrice decimal.Decimal
	StopLossPrice    decimal.Decimal
}

// SpotFollowerHistoryOrder — one closed copied spot order
// (GET spot-follower/query-history-orders). AchievedPLR is a percentage.
type SpotFollowerHistoryOrder struct {
	TrackingNo string
	TraderID   string
	Symbol     string

	FillSize    decimal.Decimal
	BuyPrice    decimal.Decimal
	SellPrice   decimal.Decimal
	BuyFee      decimal.Decimal
	SellFee     decimal.Decimal
	BuyTimeMs   int64
	SellTimeMs  int64
	AchievedPL  decimal.Decimal
	AchievedPLR decimal.Decimal
}

// SpotFollowSetting — one per-symbol follow-configuration row sent to
// POST spot-follower/settings. Strings (not decimals) on purpose so the
// caller can distinguish "omit" (empty) from an explicit value.
//
// Required: Symbol, TraceType, MaxHoldSize, TraceValue.
//
//	TraceType: "percent" | "amount" | "count"
type SpotFollowSetting struct {
	Symbol           string
	TraceType        string
	MaxHoldSize      string
	TraceValue       string
	StopSurplusRatio string
	StopLossRatio    string
}

// SpotFollowSettingsRequest — input to POST spot-follower/settings.
//
//	AutoCopy: "on" | "off"   (basic mode only)
//	Mode:     "basic" | "advanced" (default advanced)
type SpotFollowSettingsRequest struct {
	TraderID string
	AutoCopy string
	Mode     string
	Settings []SpotFollowSetting
}

// SpotFollowSettingDetail — one row of tradeSettingList in
// GET spot-follower/query-settings (the follower's active config).
type SpotFollowSettingDetail struct {
	Symbol            string
	TraceType         string
	MaxTraceAmount    decimal.Decimal
	StopLossRation    decimal.Decimal
	StopSurplusRation decimal.Decimal
}

// SpotFollowSymbolBounds — one row of tradeSymbolSettingList in
// GET spot-follower/query-settings: the venue min/max bounds the follower
// must respect when configuring a symbol. Ratios are percentages.
type SpotFollowSymbolBounds struct {
	Symbol string

	MaxTraceAmount       decimal.Decimal
	MaxTraceAmountSystem decimal.Decimal
	MaxTraceSize         decimal.Decimal
	MaxTraceRation       decimal.Decimal
	MinTraceAmount       decimal.Decimal
	MinTraceSize         decimal.Decimal
	MinTraceRation       decimal.Decimal

	MaxStopLossRation    decimal.Decimal
	MaxStopSurplusRation decimal.Decimal
	MinStopLossRation    decimal.Decimal
	MinStopSurplusRation decimal.Decimal

	SliderMaxStopLossRatio    decimal.Decimal
	SliderMaxStopSurplusRatio decimal.Decimal
}

// SpotFollowSettings — response of GET spot-follower/query-settings.
// Following mirrors enable ("YES"/"NO") as a bool.
type SpotFollowSettings struct {
	Following     bool
	ProfitRate    decimal.Decimal
	SettledInDays int64
	TraderName    string
	TraderHeadPic string

	TradeSettings       []SpotFollowSettingDetail
	TradeSymbolSettings []SpotFollowSymbolBounds
}

// SpotFollowTPSLRequest — input to POST spot-follower/setting-tpsl.
// Prices are strings to preserve the empty / "0" / >0 semantics.
type SpotFollowTPSLRequest struct {
	TrackingNo       string
	StopSurplusPrice string
	StopLossPrice    string
}
