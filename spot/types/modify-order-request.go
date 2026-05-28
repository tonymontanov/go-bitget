/*
FILE: spot/types/modify-order-request.go

DESCRIPTION:
ModifyOrderRequest — input to spot.TradingClient.ModifyOrder /
ModifyBatchOrders. Targets POST /api/v2/spot/trade/cancel-replace-order
(and /batch-cancel-replace-order on the batch endpoint).

THE BITGET MODIFY-ORDER QUIRK:

Same shape as on mix: Bitget implements modify as a cancel-replace at
the matcher level, so the resulting order needs a fresh customer ID.
Reusing the existing clientOid yields code=40786 "Duplicate clientOid"
and the modify is rejected. The SDK auto-fills NewClientOrderID with a
collision-resistant `s-<32-hex>` token via crypto/rand when the caller
left it empty (mirroring the mix `m-<32-hex>` convention; the prefix
discriminates spot- vs mix-side IDs in audit logs).

NATIVE BATCH-MODIFY ON SPOT:

Unlike mix (where Bitget V2 has no native batch-modify endpoint and
the SDK fans the call out client-side), spot ships a NATIVE batch-
cancel-replace-order. spot.TradingClient.ModifyBatchOrders therefore
issues a single REST RPC per batch — no fan-out, no concurrency cap,
no extra latency from N round-trips.

IDENTIFICATION:

Either OrderID or ClientOrderID points at the existing order. If
both are populated, OrderID wins (Bitget's documented precedence
rule).

NewQuantity / NewPrice: at least one must be non-zero. Decimal-zero
fields are passed through as "absent" to keep the original value.
*/

package types

import "github.com/shopspring/decimal"

// ModifyOrderRequest — parameters for amending one spot order.
type ModifyOrderRequest struct {
	Symbol           string
	OrderID          string
	ClientOrderID    string
	NewClientOrderID string
	NewQuantity      decimal.Decimal
	NewPrice         decimal.Decimal
}
