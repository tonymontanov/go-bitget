/*
FILE: spot/types/fill-update.go

DESCRIPTION:
FillUpdate — one trade execution streamed over the V2 spot `fill`
private WS channel. Distinct from the REST `Fill` shape in this same
package: the WS push ships a `feeDetail[]` ARRAY (so a single execution
can carry both a primary fee and a deduction credit, e.g. BGB rebate
on configured accounts) where REST collapses that into TotalFee +
FeeCoin singletons.

WHY A SEPARATE TYPE:

Same reason the WS ticker shape lives next to MarketTicker rather than
folding into it: the wire contract on the channel is a superset (fee-
detail array + uTime) of the REST endpoint's contract. Sharing one
shape would either always-zero half the WS-only fields on REST callers
or force REST users to walk a one-element FeeDetail slice for the
common single-fee case. Profile-local types is the same anti-bloat
rule we follow for tickers.

WHY NO clientOid:

Bitget V2 spot's fill channel does NOT ship clientOid (verified
against the official docs). Callers that need to correlate fills to
their own idempotency keys MUST go through the orders push (which
DOES carry clientOid) and join on OrderID. This is a venue-level
invariant; an SDK workaround would need stateful cross-channel
tracking, deferred until a desk surfaces a need.
*/

package types

import (
	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// FillUpdate — one execution row from the spot `fill` private channel.
type FillUpdate struct {
	// OrderID — the order this execution belongs to.
	OrderID string
	// TradeID — venue-assigned ID, unique per execution.
	TradeID string
	// Symbol — e.g. "BTCUSDT".
	Symbol string

	// Side — buy / sell.
	Side roottypes.SideType
	// OrderType — limit / market.
	OrderType roottypes.OrderType

	// PriceAvg — actual matched price for this execution. Spot wire
	// names this `priceAvg` (vs mix's `price`); the SDK keeps the
	// venue name to make wire ↔ struct mapping mechanical.
	PriceAvg decimal.Decimal
	// Size — base-asset quantity matched on this execution. Wire
	// name `size` (vs mix's `baseVolume`).
	Size decimal.Decimal
	// Amount — accumulated quote-asset volume on this execution. Wire
	// name `amount` (vs mix's `quoteVolume`). Equals PriceAvg * Size
	// to within Bitget's rounding rules.
	Amount decimal.Decimal

	// TradeScope — "taker" / "maker"; raw string because Bitget
	// occasionally extends the set (e.g. "self_trade" / "liquidation").
	TradeScope string

	// FeeDetail — the venue's full fee breakdown for this execution.
	// Typically length 1 (one fee row, no deduction); the array is
	// kept verbatim so callers that account for BGB / coupon
	// deductions get the totals they need.
	FeeDetail []bgcommon.WSFeeDetail

	// CreatedAtMs — execution time at the venue (ms since epoch).
	CreatedAtMs int64
	// UpdatedAtMs — push update time. Equal to CreatedAtMs for the
	// initial event; differs only on rare server-side corrections.
	UpdatedAtMs int64
}
