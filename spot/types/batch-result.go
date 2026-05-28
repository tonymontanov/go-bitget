/*
FILE: spot/types/batch-result.go

DESCRIPTION:
BatchOrderResult — per-row outcome on Bitget V2 spot batch trading
endpoints (/api/v2/spot/trade/batch-orders,
/api/v2/spot/trade/batch-cancel-replace-order,
/api/v2/spot/trade/cancel-batch-orders).

WIRE FORMAT:

Bitget returns two parallel arrays in the response payload:
  - successList — accepted orders (OrderID + ClientOrderID per row);
  - failureList — rejected orders (per-row Code + Msg + ClientOrderID).

The SDK collapses both into a single slice ordered like the input
request, so the caller can index by request position. Each entry has
either Order set (success) or Err set (failure), never both. Identical
shape to mix.BatchOrderResult — kept profile-local because Order is a
*spot.types.OrderInfo (different struct from mix's, even though every
field that exists on both has the same name and type).
*/

package types

// BatchOrderResult — outcome of one row in a batch trading call.
type BatchOrderResult struct {
	// Order is the order info for the accepted row. At minimum
	// OrderID + ClientOrderID; the SDK echoes back as much of the
	// original request (side / orderType / timeInForce / quantity /
	// price) as it can to keep the result self-describing without an
	// extra GET.
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
