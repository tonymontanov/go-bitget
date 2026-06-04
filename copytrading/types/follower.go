/*
FILE: copytrading/types/follower.go

DESCRIPTION:
Domain types for the FOLLOWER side of copy trading. M2 covers the
futures follower surface; the spot follower (M4) reuses the directory
row and adds its own order shapes.

Field mapping verified against the Bitget V2 follower docs
(query-traders / query-current-orders / query-history-orders /
close-positions / cancel-trader).
*/

package types

import "github.com/shopspring/decimal"

// Trader — one row of the follower "my traders" directory
// (GET mix-follower/query-traders). Counts (follow limits / counts) are
// whole numbers; margin / profit fields are decimals. CurrentTradingPairs
// lists the symbols the trader is currently copy-trading.
type Trader struct {
	TraderID          string
	TraderName        string
	CertificationType string // "Certified" / "Uncertified"

	MaxFollowLimit    int64
	FollowCount       int64
	BGBMaxFollowLimit int64
	BGBFollowCount    int64

	TraceTotalMarginAmount decimal.Decimal
	TraceTotalNetProfit    decimal.Decimal
	TraceTotalProfit       decimal.Decimal

	CurrentTradingPairs []string
	FollowerTimeMs      int64
}

// FollowerCurrentOrder — one live copied position
// (GET mix-follower/query-current-orders). PosSide is long/short in
// hedge mode. Close* fields are populated as the position is being
// wound down.
type FollowerCurrentOrder struct {
	TrackingNo   string
	TraderID     string
	TraderName   string
	OpenOrderID  string
	CloseOrderID string
	Symbol       string
	PosSide      string

	OpenLeverage   decimal.Decimal
	OpenPriceAvg   decimal.Decimal
	OpenSize       decimal.Decimal
	OpenMarginSize decimal.Decimal
	OpenFee        decimal.Decimal
	OpenTimeMs     int64

	CloseAvgPrice decimal.Decimal
	CloseSize     decimal.Decimal
	CloseTimeMs   int64
}

// FollowerHistoryOrder — one closed copied position
// (GET mix-follower/query-history-orders). Adds the realised
// profit/loss fields the current-orders view omits.
type FollowerHistoryOrder struct {
	TrackingNo   string
	TraderID     string
	OpenOrderID  string
	CloseOrderID string
	ProductType  string
	Symbol       string
	PosSide      string

	OpenLeverage  decimal.Decimal
	OpenPriceAvg  decimal.Decimal
	OpenSize      decimal.Decimal
	OpenFee       decimal.Decimal
	OpenTimeMs    int64
	ClosePriceAvg decimal.Decimal
	CloseSize     decimal.Decimal
	CloseFee      decimal.Decimal
	CloseTimeMs   int64

	ProfitRate decimal.Decimal
	NetProfit  decimal.Decimal
	AchievedPL decimal.Decimal
}

// CloseFollowerPositionsRequest — input to the follower close-positions
// endpoint. All fields are optional EXCEPT that, per Bitget, when
// TrackingNo is set together with Symbol/MarginCoin/MarginMode/HoldSide
// they must be mutually consistent. Empty TrackingNo + empty Symbol
// closes every copied position. HoldSide is required only in hedge
// mode. ProductType is taken from the client (pinned), not this struct.
type CloseFollowerPositionsRequest struct {
	TrackingNo string
	Symbol     string
	MarginCoin string
	MarginMode string // isolated / crossed
	HoldSide   string // long / short (hedge mode only)
}

// CloseResult — response of the follower / trader close endpoints
// (close-positions / order-close-positions). Bitget returns the list of
// venue order IDs the close generated.
type CloseResult struct {
	OrderIDList []string
}

// SymbolSetting — one per-symbol follow-configuration row sent to
// POST mix-follower/settings. Strings (not decimals) on purpose: several
// fields (ratios, leverage) distinguish "omit / leave unchanged" (empty)
// from an explicit value, which a decimal zero-value would erase. The
// caller formats numbers as it sees fit. ProductType is taken from the
// client (pinned), not this struct.
//
// Required: Symbol, MarginType, LeverType, TraceType, TraceValue.
//
//	MarginType: "trader" | "specify"        (advanced mode only)
//	LeverType:  "position" | "specify" | "trader" (advanced mode only)
//	TraceType:  "percent" | "amount" | "count"
type SymbolSetting struct {
	Symbol           string
	MarginType       string
	MarginCoin       string
	LeverType        string
	LongLeverage     string
	ShortLeverage    string
	TraceType        string
	TraceValue       string
	MaxHoldSize      string
	StopSurplusRatio string
	StopLossRatio    string
}

// FollowSettingsRequest — input to POST mix-follower/settings. Sets the
// copy-trade configuration (per symbol, max 10) for one lead trader.
//
//	AutoCopy: "on" | "off"   (basic mode only)
//	Mode:     "basic" | "advanced" (default advanced)
type FollowSettingsRequest struct {
	TraderID string
	AutoCopy string
	Mode     string
	Settings []SymbolSetting
}

// FollowSettingDetail — one per-symbol row returned by
// GET mix-follower/query-settings. Numeric outputs are decimals; the
// classifier fields stay strings.
type FollowSettingDetail struct {
	Symbol           string
	ProductType      string
	MarginType       string
	MarginCoin       string
	LeverType        string
	LongLeverage     decimal.Decimal
	ShortLeverage    decimal.Decimal
	TraceType        string
	TraceValue       decimal.Decimal
	MaxHoldSize      decimal.Decimal
	StopSurplusRatio decimal.Decimal
	StopLossRatio    decimal.Decimal
}

// FollowSettings — response of GET mix-follower/query-settings.
// FollowerEnable is "YES"/"NO"; Following mirrors it as a bool for
// convenience.
type FollowSettings struct {
	FollowerEnable string
	Following      bool
	DetailList     []FollowSettingDetail
}

// FollowTPSLRequest — input to POST mix-follower/setting-tpsl. Prices are
// strings to preserve Bitget's three-way semantics: empty = leave
// unchanged, "0" = cancel an existing TP/SL, > 0 = set/update.
// ProductType is taken from the client (pinned).
type FollowTPSLRequest struct {
	TrackingNo       string
	Symbol           string
	StopSurplusPrice string
	StopLossPrice    string
}

// FollowLimit — one row of GET mix-follower/query-quantity-limit: the
// min/max copy order size (in trading currency) for a symbol.
type FollowLimit struct {
	Symbol        string
	MaxFollowSize decimal.Decimal
	MinFollowSize decimal.Decimal
}
