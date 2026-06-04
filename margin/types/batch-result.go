/*
FILE: margin/types/batch-result.go

DESCRIPTION:
BatchOrderResult — per-row outcome on the Bitget V2 margin batch
trading endpoints (/api/v2/margin/<mode>/batch-place-order and
/batch-cancel-order).

Bitget returns the standard {successList, failureList} envelope; the
SDK collapses it into a single slice ordered like the input request so
the caller can index by request position. Each entry has either Order
set (success) or Err set (failure), never both. ClientOrderID is set on
every row so the caller can correlate even on pre-matcher rejections.

Identical shape to spot.BatchOrderResult, kept profile-local because
Order is a *margin.types.OrderInfo (distinct struct).
*/

package types

// BatchOrderResult — outcome of one row in a margin batch trading call.
type BatchOrderResult struct {
	Order         *OrderInfo
	Err           error
	ClientOrderID string
}
