/*
FILE: mix/types/order-info.go

DESCRIPTION:
OrderInfo — view of one order's lifecycle state, returned by
GET /api/v2/mix/order/detail, GET /api/v2/mix/order/orders-pending,
POST /api/v2/mix/order/place-order and the WS "orders" channel.

M1 ships the SHAPE so callers and the desk adapter can compile
against the final type. The trading and account sub-clients are
stubbed in M1; M2 wires create/modify/cancel and M3 wires the query
endpoints — both will populate this struct.

FIELD CHOICES:
  - OrderID         : Bitget exchange ID. Always non-empty post-accept.
  - ClientOrderID   : pass-through from CreateOrderRequest. The SDK
    keeps an OrderID ↔ ClientOrderID cache so both
    are populated on every event regardless of what
    Bitget originally returned.
  - Symbol/Side/HoldSide/Type/Force/Status : straight wire mapping
    using the typed enums in enums.go.
  - Quantity / Price / FilledQuantity / AvgFilledPrice : decimal.
  - CreatedAtMs / UpdatedAtMs : exchange timestamps; updates after
    every state transition.
*/

package types

import (
	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// OrderInfo — order lifecycle snapshot.
type OrderInfo struct {
	OrderID        string
	ClientOrderID  string
	Symbol         string
	Side           roottypes.SideType
	TradeSide      roottypes.TradeSide
	HoldSide       HoldSide
	OrderType      roottypes.OrderType
	TimeInForce    roottypes.TimeInForceType
	Status         roottypes.OrderStatus
	Quantity       decimal.Decimal
	Price          decimal.Decimal
	FilledQuantity decimal.Decimal
	AvgFilledPrice decimal.Decimal
	CumFee         decimal.Decimal
	CreatedAtMs    int64
	UpdatedAtMs    int64
}
