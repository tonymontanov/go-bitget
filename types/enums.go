/*
FILE: types/enums.go

DESCRIPTION:
Closed enums (typed strings) shared across every Bitget profile (mix,
spot, uta). Values match the wire format Bitget accepts and returns —
keep them exact, or the exchange rejects with code 40017 / 40034.

Profile packages re-export these via type aliases (see mix/types,
spot/types). Profile-specific values that the exchange ALSO encodes as
the same enum type are added by the profile package as constants of the
aliased type — no separate enum is introduced.
*/

package types

// ProductType — Bitget contract family identifier sent on every MIX REST
// call (`productType` query/body parameter). Each profile pins one value
// at the call site, but the constant set lives here so that the rate-
// limiter / observer code can address every product type uniformly
// without depending on a profile package.
type ProductType string

const (
	// ProductTypeUSDTFutures — USDT-margined perpetuals (default for
	// mix v1.0).
	ProductTypeUSDTFutures ProductType = "USDT-FUTURES"
	// ProductTypeCoinFutures — coin-margined perpetuals.
	ProductTypeCoinFutures ProductType = "COIN-FUTURES"
	// ProductTypeUSDCFutures — USDC-margined perpetuals.
	ProductTypeUSDCFutures ProductType = "USDC-FUTURES"
	// ProductTypeSusdtFutures — SUSDT-margined demo perpetuals (testnet).
	ProductTypeSusdtFutures ProductType = "SUSDT-FUTURES"
	// ProductTypeScoinFutures — SCOIN-margined demo perpetuals (testnet).
	ProductTypeScoinFutures ProductType = "SCOIN-FUTURES"
	// ProductTypeSusdcFutures — SUSDC-margined demo perpetuals (testnet).
	ProductTypeSusdcFutures ProductType = "SUSDC-FUTURES"
)

// SideType — order direction on the exchange wire. Bitget uses lower-case
// strings on the V2 / MIX endpoints.
type SideType string

const (
	// SideTypeBuy — buy / long.
	SideTypeBuy SideType = "buy"
	// SideTypeSell — sell / short.
	SideTypeSell SideType = "sell"
)

// TradeSide — Bitget extension used by the MIX endpoints when the account
// is in hedge ("two-way") mode. In one-way mode the field is omitted.
//
// Combined with SideType the four legal pairs are:
//
//	(buy , open ) — open long
//	(sell, open ) — open short
//	(buy , close) — close short
//	(sell, close) — close long
type TradeSide string

const (
	// TradeSideOpen — open a new position leg (hedge mode).
	TradeSideOpen TradeSide = "open"
	// TradeSideClose — close an existing position leg (hedge mode).
	TradeSideClose TradeSide = "close"
)

// OrderType — order execution model on the exchange wire. Bitget uses
// lower-case strings.
type OrderType string

const (
	// OrderTypeLimit — limit order.
	OrderTypeLimit OrderType = "limit"
	// OrderTypeMarket — market order.
	OrderTypeMarket OrderType = "market"
)

// TimeInForceType — order expiry / queue behaviour. Bitget recognises
// post_only on limit orders only — the SDK applies that mapping in
// trading.go of each profile.
type TimeInForceType string

const (
	// TimeInForceGTC — Good Till Cancel (default for limit).
	TimeInForceGTC TimeInForceType = "gtc"
	// TimeInForceIOC — Immediate or Cancel (default for market).
	TimeInForceIOC TimeInForceType = "ioc"
	// TimeInForceFOK — Fill or Kill.
	TimeInForceFOK TimeInForceType = "fok"
	// TimeInForcePostOnly — post-only (rejected if it would cross the
	// book). Maps to orderType=limit + timeInForce=post_only on the wire.
	TimeInForcePostOnly TimeInForceType = "post_only"
)

// OrderStatus — base order states emitted by Bitget on the V2 / MIX
// endpoints. Bitget occasionally extends the catalogue; values outside
// the well-known set are returned verbatim in OrderInfo.Status —
// callers that need finer status discrimination should read the raw
// status string.
type OrderStatus string

const (
	// OrderStatusLive — accepted by the matcher and resting in the book.
	OrderStatusLive OrderStatus = "live"
	// OrderStatusNew — alias for Live emitted on some endpoints.
	OrderStatusNew OrderStatus = "new"
	// OrderStatusPartiallyFilled — partially filled, remainder live.
	OrderStatusPartiallyFilled OrderStatus = "partially_filled"
	// OrderStatusFilled — fully filled.
	OrderStatusFilled OrderStatus = "filled"
	// OrderStatusCancelled — cancelled (also: "canceled" on some channels).
	OrderStatusCancelled OrderStatus = "cancelled"
	// OrderStatusRejected — rejected by the exchange before reaching the book.
	OrderStatusRejected OrderStatus = "rejected"
)

// PositionMode — global account flag selecting one-way vs. hedge mode for
// MIX trading.
type PositionMode string

const (
	// PositionModeOneWay — single-direction position; tradeSide is
	// omitted on order placement. SDK v1.0 default.
	PositionModeOneWay PositionMode = "one_way_mode"
	// PositionModeHedge — separate long / short positions; tradeSide
	// must be specified on every order.
	PositionModeHedge PositionMode = "hedge_mode"
)

// MarginMode — per-symbol margining mode. Bitget V2 uses lower-case
// strings on the wire.
type MarginMode string

const (
	// MarginModeIsolated — isolated margin (per-symbol).
	MarginModeIsolated MarginMode = "isolated"
	// MarginModeCrossed — crossed margin (shared across symbols of the
	// same product type).
	MarginModeCrossed MarginMode = "crossed"
)
