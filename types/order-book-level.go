/*
FILE: types/order-book-level.go

DESCRIPTION:
A single order book level — protocol-common across every Bitget profile.
Used by:
  - REST snapshot (GET /api/v2/mix/market/depth, /api/v2/spot/market/depth);
  - the SDK orderbook engine (snapshot/delta application);
  - WebSocket "books"/"books5"/"books15" channel dispatch.

Bitget represents a level as a positional [price, size] pair of strings:
["27045.00", "0.123"]. The SDK normalises both parts into decimal.Decimal
at the boundary.
*/

package types

import "github.com/shopspring/decimal"

// OrderBookLevel — one order book level.
type OrderBookLevel struct {
	Price decimal.Decimal
	Size  decimal.Decimal
}
