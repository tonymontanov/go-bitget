/*
FILE: mix/types/modify-order-request.go

DESCRIPTION:
ModifyOrderRequest — input to TradingClient.ModifyOrder. Bitget MIX
identifies the existing order by either OrderID or ClientOrderID,
and assigns the modified order a NEW clientOid (`newClientOid`).
At least one of NewQuantity / NewPrice must be set.

THE BITGET MODIFY-ORDER QUIRK:
Per the official spec
(https://www.bitget.com/api-doc/contract/trade/Modify-Order) the
`newClientOid` field is REQUIRED and MUST NOT equal the existing
clientOid — internally Bitget implements modify as a cancel-replace
at the matcher level, so the resulting order needs a fresh customer
ID. Reusing the same value yields code=40786 "Duplicate clientOid"
and the modify is rejected (regression seen in PARTIUSDT field log
v1.0.3).

The SDK accepts an explicit NewClientOrderID for callers that want
to own the ID space (deduplication, idempotency tracking, parent
strategy correlation). When left empty, the SDK auto-generates a
collision-resistant `m-` + 16-hex-byte token via crypto/rand so
the modify always succeeds without forcing every caller to bring
their own UUID generator.
*/

package types

import "github.com/shopspring/decimal"

// ModifyOrderRequest — parameters for amending one MIX order.
//
// Identity (one of, OrderID wins if both set):
//   - OrderID: server-assigned order ID, returned by CreateOrder.
//   - ClientOrderID: customer-assigned ID supplied at create time.
//
// NewClientOrderID:
//   - Customer-assigned ID for the MODIFIED order (Bitget's
//     `newClientOid`, REQUIRED by the venue, MUST differ from the
//     existing ClientOrderID).
//   - If empty, the SDK fills in a generated token of the form
//     `m-<16-hex>` so callers don't have to manage IDs manually.
//
// NewQuantity / NewPrice: at least one must be non-zero. Decimal-zero
// fields are passed through as "absent" to keep the original value.
type ModifyOrderRequest struct {
	Symbol           string
	OrderID          string
	ClientOrderID    string
	NewClientOrderID string
	NewQuantity      decimal.Decimal
	NewPrice         decimal.Decimal
}
