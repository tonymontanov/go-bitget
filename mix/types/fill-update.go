/*
FILE: mix/types/fill-update.go

DESCRIPTION:
FillUpdate — one trade execution streamed over the V2 mix `fill`
private WS channel. Distinct from the spot counterpart in spot/types/
fill-update.go: mix carries derivatives-only fields (clientOid,
posMode, tradeSide, profit) plus uses different wire field names for
the same concepts (price / baseVolume / quoteVolume vs spot's
priceAvg / size / amount).

WHY A SEPARATE TYPE:

A shared FillUpdate would either bloat spot with always-zero
derivatives fields or omit half of mix's data. Same anti-pattern we
avoided for the ticker channel in M4. Wire shapes are profile-local;
the only shared piece is the FeeDetail array, which lives in the
shared bgcommon package and is consumed by both profiles.
*/

package types

import (
	"github.com/shopspring/decimal"

	"github.com/tonymontanov/go-bitget/v2/internal/bgcommon"
	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// FillUpdate — one execution row from the mix `fill` private channel.
type FillUpdate struct {
	// OrderID — the order this execution belongs to.
	OrderID string
	// ClientOrderID — caller-supplied idempotency token (mix DOES
	// ship this on the fill channel; spot does NOT).
	ClientOrderID string
	// TradeID — venue-assigned ID, unique per execution.
	TradeID string
	// Symbol — e.g. "BTCUSDT".
	Symbol string

	// Side — buy / sell. NOTE: in hedge mode, "Open Long" and
	// "Close Short" both surface as `buy`; "Close Long" and "Open
	// Short" both surface as `sell`. Use TradeSide to disambiguate.
	Side roottypes.SideType
	// OrderType — limit / market.
	OrderType roottypes.OrderType
	// PosMode — "one_way_mode" / "hedge_mode".
	PosMode string
	// TradeSide — open / close / reduce_close_long / burst_close_short
	// / ... (20+ variants; see Bitget docs). The full enum lives in
	// roottypes.TradeSide; raw string forwarded to forward-compat new
	// values without an SDK bump.
	TradeSide roottypes.TradeSide

	// Price — actual matched price for this execution. Wire name
	// `price` (vs spot's `priceAvg`).
	Price decimal.Decimal
	// BaseVolume — base-asset quantity matched. Wire name
	// `baseVolume` (vs spot's `size`).
	BaseVolume decimal.Decimal
	// QuoteVolume — quote-asset notional matched. Wire name
	// `quoteVolume` (vs spot's `amount`).
	QuoteVolume decimal.Decimal
	// Profit — realized PnL booked on this execution. Mix-only;
	// spot has no concept of realized PnL (cash-only spot).
	Profit decimal.Decimal

	// TradeScope — "taker" / "maker"; raw string because Bitget
	// occasionally extends the set.
	TradeScope string

	// FeeDetail — the venue's full fee breakdown. Same shape as on
	// spot — both profiles share bgcommon.WSFeeDetail.
	FeeDetail []bgcommon.WSFeeDetail

	// CreatedAtMs — execution time at the venue (ms since epoch).
	CreatedAtMs int64
	// UpdatedAtMs — push update time. Equal to CreatedAtMs for the
	// initial event; differs only on rare server-side corrections.
	UpdatedAtMs int64
}
