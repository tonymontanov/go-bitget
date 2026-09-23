/*
FILE: uta/types/stream.go

DESCRIPTION:
Domain types delivered by the V3 UTA PUBLIC WebSocket (uta.StreamClient):
ticker, public trades and order-book updates. Field mapping verified
against live frames captured from wss://ws.bitget.com/v3/ws/public
(2026-09-21) and the Bitget V3 WebSocket docs. decimal for prices / sizes,
int64 epoch-ms for timestamps.

The PRIVATE topics (order / fill / position / account) reuse the REST
domain types — Order, Fill, CurrentPosition, AccountAssets — so a consumer
handles one shape regardless of the transport. Where the WS row carries
more than the REST row those types were extended additively; see the
"WS" notes on their fields.
*/

package types

import "github.com/shopspring/decimal"

// TickerUpdate — one push of the `ticker` topic. The venue pushes the full
// ticker (action=snapshot) on every change, at most every ~200 ms.
type TickerUpdate struct {
	// Category — the category the caller subscribed with (canonical
	// upper-case constant, e.g. CategoryUSDTFutures).
	Category Category
	Symbol   string

	LastPrice decimal.Decimal
	Bid1Price decimal.Decimal
	Bid1Size  decimal.Decimal
	Ask1Price decimal.Decimal
	Ask1Size  decimal.Decimal

	// Futures-specific (zero on spot).
	MarkPrice   decimal.Decimal
	IndexPrice  decimal.Decimal
	FundingRate decimal.Decimal
	// NextFundingTimeMs — next funding settlement, epoch-ms (0 on spot).
	NextFundingTimeMs int64

	// TsMs — push timestamp from the frame envelope, epoch-ms.
	TsMs int64
}

// PublicTradeUpdate — one public trade of the `publicTrade` topic. The
// SDK skips the history snapshot the venue sends on subscribe and fans a
// multi-trade frame out oldest-first, one call per trade.
type PublicTradeUpdate struct {
	// Category — the category the caller subscribed with.
	Category Category
	Symbol   string

	TradeID string
	Price   decimal.Decimal
	// Size — base-coin quantity (quote-coin on COIN-FUTURES, per venue docs).
	Size decimal.Decimal
	// Side — TAKER side: "buy" | "sell".
	Side string
	// IsRPI — the trade is a Retail Price Improvement fill.
	IsRPI bool
	// TsMs — trade time (wire field T), epoch-ms.
	TsMs int64
}

// OrderBookUpdate — the order book after one applied push of a `books*`
// topic. Every delivery is a FULL view of the top levels (never a delta):
// books1 / books5 / books50 are stateless venue snapshots, the full-depth
// `books` topic is maintained locally by the SDK and validated by the
// seq / pseq chain.
//
// Asks ascend, bids descend, best level first, at most the requested
// `depth` levels per side. The slices are freshly allocated per push and
// never modified by the SDK afterwards, so they may be retained; they are
// SHARED between the handlers of one subscription — treat them as
// read-only.
type OrderBookUpdate struct {
	// Category — the category the caller subscribed with.
	Category Category
	Symbol   string

	Asks []PriceLevel
	Bids []PriceLevel

	// Seq — venue sequence number of the applied frame.
	Seq int64
	// TsMs — system generation time of the data (data[].ts), falling back
	// to the envelope push timestamp; epoch-ms.
	TsMs int64
}
