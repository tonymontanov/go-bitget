/*
FILE: spot/types/fill.go

DESCRIPTION:
Fill — one trade execution returned by GET /api/v2/spot/trade/fills.
Bitget reports trades from the API key's perspective (one Fill per
maker-taker match-up the account participated in), independent of
how the originating order was placed.

USES:

  - Post-trade reconciliation: replay the desk's trade log against
    Bitget's authoritative fills.
  - Effective spread / realised slippage analysis (FillPrice vs.
    the order's intended Price).
  - Fee accounting: TotalFee + FeeCoin land here verbatim.

DIFFERENCES FROM mix:

There is no native fill-history endpoint on the legacy mix V2 API —
mix users derive fills from the WS "fills" channel. Spot ships a
dedicated REST endpoint, so the SDK has a typed REST helper here
that mix lacks.

NUMERIC HANDLING:

Same conventions as elsewhere — shopspring/decimal for amounts /
prices / fees, int64 ms for timestamps, raw string for enum-ish
fields where Bitget extends the set without API-version bumps.
*/

package types

import (
	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// Fill — one trade execution from /api/v2/spot/trade/fills.
type Fill struct {
	// OrderID — the order this fill belongs to.
	OrderID string
	// TradeID — the venue-assigned trade ID for this match. Unique
	// per fill.
	TradeID string
	// Symbol — e.g. "BTCUSDT".
	Symbol string

	// Side — buy / sell (the API-key side of this match).
	Side roottypes.SideType
	// OrderType — limit / market.
	OrderType roottypes.OrderType

	// FillPrice — actual matched price. Differs from the originating
	// order's price when the resting taker side had better levels
	// (price improvement) or when the order was market-type.
	FillPrice decimal.Decimal
	// Size — base-asset quantity matched on this fill.
	Size decimal.Decimal
	// Amount — quote-asset notional (= FillPrice * Size).
	Amount decimal.Decimal

	// TotalFee — absolute fee charged on this fill, denominated in
	// FeeCoin. Sign convention follows Bitget: NEGATIVE means the
	// venue debited the account (the typical case); positive means
	// a maker rebate.
	TotalFee decimal.Decimal
	// FeeCoin — currency the fee is denominated in (typically the
	// quote coin for sells, the base coin for buys, but Bitget
	// permits BGB-deduction on configured accounts).
	FeeCoin string

	// TradeScope — "taker" or "maker"; raw string because Bitget
	// occasionally extends the set (e.g. "self_trade" / "liquidation").
	TradeScope string

	// CreatedAtMs — venue execution timestamp (ms since epoch).
	CreatedAtMs int64
}
