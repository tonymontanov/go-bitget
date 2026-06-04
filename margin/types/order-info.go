/*
FILE: margin/types/order-info.go

DESCRIPTION:
OrderInfo — view of one margin order's lifecycle state, returned by
place-order, the open / history order queries and the private WS
"orders-<mode>" channel.

Margin orders are SPOT instruments funded with borrowed collateral, so
the shape closely follows spot/types.OrderInfo. The margin-only addition
is LoanType (the auto-borrow / auto-repay flag the order was placed with),
which Bitget echoes on the query / WS rows.
*/

package types

import (
	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// OrderInfo — order lifecycle snapshot for one margin order.
type OrderInfo struct {
	OrderID        string
	ClientOrderID  string
	Symbol         string
	Side           roottypes.SideType
	OrderType      roottypes.OrderType
	TimeInForce    roottypes.TimeInForceType
	Status         roottypes.OrderStatus
	LoanType       LoanType
	Quantity       decimal.Decimal
	Price          decimal.Decimal
	FilledQuantity decimal.Decimal
	AvgFilledPrice decimal.Decimal
	CumFee         decimal.Decimal
	CreatedAtMs    int64
	UpdatedAtMs    int64
}
