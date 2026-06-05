/*
FILE: uta/types/enums.go

DESCRIPTION:
Shared V3 UTA enums. The defining trait of the V3 surface is the unified
`category` parameter — one client serves SPOT / MARGIN and all three
futures product types, selected per call rather than pinned at
construction (unlike the V2 mix/ profile's ClientSettings).
*/

package types

// Category — the V3 product category passed to most market / trade
// endpoints. Not every endpoint accepts every value (futures-only
// endpoints reject SPOT / MARGIN); the SDK forwards the caller's choice
// and lets the venue validate.
type Category string

const (
	// CategorySpot — spot trading.
	CategorySpot Category = "SPOT"
	// CategoryMargin — spot margin (leveraged spot).
	CategoryMargin Category = "MARGIN"
	// CategoryUSDTFutures — USDT-margined perpetual / delivery futures.
	CategoryUSDTFutures Category = "USDT-FUTURES"
	// CategoryCOINFutures — coin-margined futures.
	CategoryCOINFutures Category = "COIN-FUTURES"
	// CategoryUSDCFutures — USDC-margined futures.
	CategoryUSDCFutures Category = "USDC-FUTURES"
)

// Interval — candlestick granularity. V3 uses upper-case H / D suffixes.
type Interval string

const (
	Interval1m  Interval = "1m"
	Interval3m  Interval = "3m"
	Interval5m  Interval = "5m"
	Interval15m Interval = "15m"
	Interval30m Interval = "30m"
	Interval1H  Interval = "1H"
	Interval4H  Interval = "4H"
	Interval6H  Interval = "6H"
	Interval12H Interval = "12H"
	Interval1D  Interval = "1D"
)

// HoldMode — futures position mode (Account.SetHoldMode).
type HoldMode string

const (
	// HoldModeOneWay — single net position per symbol (no posSide).
	HoldModeOneWay HoldMode = "one_way_mode"
	// HoldModeHedge — separate long / short positions (orders carry posSide).
	HoldModeHedge HoldMode = "hedge_mode"
)

// CandleType — which price series a candle query returns.
type CandleType string

const (
	// CandleTypeMarket — traded (market) price candles. Default.
	CandleTypeMarket CandleType = "MARKET"
	// CandleTypeMark — mark price candles.
	CandleTypeMark CandleType = "MARK"
	// CandleTypeIndex — index price candles.
	CandleTypeIndex CandleType = "INDEX"
)
