/*
FILE: mix/types/batch-result.go

DESCRIPTION:
BatchOrderResult — per-row outcome on Bitget MIX batch trading
endpoints (/api/v2/mix/order/batch-place-order,
/api/v2/mix/order/batch-modify-order,
/api/v2/mix/order/batch-cancel-orders).

WIRE FORMAT:
Bitget returns two parallel arrays in the response payload:
  - successList — accepted orders (OrderID + ClientOrderID per row);
  - failureList — rejected orders (per-row Code + Msg + ClientOrderID).

The SDK collapses both into a single slice ordered like the input
request, so the caller can index by request position. Each entry has
either Order set (success) or Err set (failure), never both.

M1 ships the SHAPE; M2 wires the batch REST endpoints.
*/

package types

// BatchOrderResult — outcome of one row in a batch trading call.
type BatchOrderResult struct {
	// Order is the order info for the accepted row (M2 will populate
	// what Bitget returns: at minimum OrderID + ClientOrderID; full
	// OrderInfo when the API embeds it).
	Order *OrderInfo
	// Err is set when Bitget rejected this specific row. Maps to a
	// *bitget.Error wrapped in the standard SDK envelope (BitgetCode +
	// Message + Kind).
	Err error
	// ClientOrderID is set on every row so the caller can correlate
	// results with the original request even when the row was rejected
	// before the order entered the matcher.
	ClientOrderID string
}
