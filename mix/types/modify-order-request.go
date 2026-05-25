/*
FILE: mix/types/modify-order-request.go

DESCRIPTION:
ModifyOrderRequest — input to TradingClient.ModifyOrder. M1 ships the
shape; M2 wires the REST call. Bitget MIX requires identifying the
order by either OrderID or ClientOrderID, and submitting the new
quantity / price. At least one of NewQuantity / NewPrice must be set.
*/

package types

import "github.com/shopspring/decimal"

// ModifyOrderRequest — parameters for amending one MIX order.
type ModifyOrderRequest struct {
	Symbol        string
	OrderID       string
	ClientOrderID string
	NewQuantity   decimal.Decimal
	NewPrice      decimal.Decimal
}
