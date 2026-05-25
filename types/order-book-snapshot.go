/*
FILE: types/order-book-snapshot.go

DESCRIPTION:
Order book snapshot — protocol-common across every Bitget profile.
Returned by MarketData.GetOrderBook and pushed by the WebSocket
"books"/"books5"/"books15" channel with action=="snapshot".

Bitget synchronisation model:
  - Each push (snapshot or delta) carries a per-symbol "ts" timestamp
    plus an optional CRC32 checksum (the "books" channel; "books5" /
    "books15" omit it). The SDK orderbook engine validates the
    checksum on every applied delta and triggers a resync on
    mismatch.
  - The full-depth "books" channel ships an initial snapshot followed
    by incremental "update" frames; "books5"/"books15" ship snapshots
    only (no diffing required).

FIELDS:
  - Symbol   — e.g. "BTCUSDT".
  - Bids     — buy levels, sorted descending by price.
  - Asks     — sell levels, sorted ascending by price.
  - TsMs     — Bitget publish timestamp (ms).
  - Checksum — CRC32 from the "books" channel; 0 when not provided.
*/

package types

// OrderBookSnapshot — order book snapshot for a single symbol.
type OrderBookSnapshot struct {
	Symbol   string
	Bids     []OrderBookLevel
	Asks     []OrderBookLevel
	TsMs     int64
	Checksum int64
}
