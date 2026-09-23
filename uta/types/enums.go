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

// ---------------------------------------------------------------------
// Order-entry vocabulary (PlaceOrderRequest & co).
//
// The request structs keep these fields as plain strings — the wire
// accepts nothing else and typed fields would break every caller that
// already builds requests from its own mapping. The constants below are
// therefore UNTYPED so `Side: types.SideBuy` compiles against a string
// field; the trade client validates the fields against this vocabulary
// before the request leaves the process, so a typo no longer round-trips
// to the venue.
//
// Values were confirmed against the live venue on 2026-09-23 (place /
// modify / cancel on USDT-FUTURES and SPOT, one-way and hedge posSide):
// they coincide with the V2 wire values.
// ---------------------------------------------------------------------

// Side — PlaceOrderRequest.Side.
const (
	SideBuy  = "buy"
	SideSell = "sell"
)

// OrderType — PlaceOrderRequest.OrderType.
const (
	OrderTypeLimit  = "limit"
	OrderTypeMarket = "market"
)

// TimeInForce — PlaceOrderRequest.TimeInForce. Empty means the venue
// default (gtc for limit, ioc for market). post_only is legal on limit
// orders only — the venue rejects it with a market order.
const (
	TimeInForceGTC      = "gtc"
	TimeInForceIOC      = "ioc"
	TimeInForceFOK      = "fok"
	TimeInForcePostOnly = "post_only"
)

// PosSide — PlaceOrderRequest.PosSide / ClosePositionsRequest.PosSide /
// SetLeverageRequest.PosSide. Required in hedge mode, must be EMPTY in
// one-way mode (the venue rejects it there).
const (
	PosSideLong  = "long"
	PosSideShort = "short"
)

// ReduceOnly — PlaceOrderRequest.ReduceOnly (futures only; the field is
// a yes/no string on the wire, not a JSON boolean).
const (
	ReduceOnlyYes = "yes"
	ReduceOnlyNo  = "no"
)

// validOrderEntryValues — vocabulary per wire field for client-side
// validation. Fields not listed here (marginMode, stpMode, trigger
// types) are forwarded verbatim: their V3 value sets were not verified
// against the live venue and a wrong allow-list would be worse than none.
var validOrderEntryValues = map[string][]string{
	"side":        {SideBuy, SideSell},
	"orderType":   {OrderTypeLimit, OrderTypeMarket},
	"timeInForce": {TimeInForceGTC, TimeInForceIOC, TimeInForceFOK, TimeInForcePostOnly},
	"posSide":     {PosSideLong, PosSideShort},
	"reduceOnly":  {ReduceOnlyYes, ReduceOnlyNo},
}

// ValidOrderEntryValue reports whether value is legal for the wire field
// (side / orderType / timeInForce / posSide / reduceOnly). An empty
// value is accepted for every field — presence is the caller's rule
// (side / orderType are required, the rest optional). Fields without a
// vocabulary are accepted verbatim.
func ValidOrderEntryValue(field, value string) bool {
	if value == "" {
		return true
	}
	var allowed, known = validOrderEntryValues[field]
	if !known {
		return true
	}
	var i int
	for i = 0; i < len(allowed); i++ {
		if allowed[i] == value {
			return true
		}
	}
	return false
}
