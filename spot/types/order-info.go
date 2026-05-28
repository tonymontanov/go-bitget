/*
FILE: spot/types/order-info.go

DESCRIPTION:
OrderInfo — view of one spot order's lifecycle state, returned by
POST /api/v2/spot/trade/orderInfo, GET /api/v2/spot/trade/unfilled-orders,
GET /api/v2/spot/trade/history-orders, POST /api/v2/spot/trade/place-order
and the WS "orders" channel.

DIFFERENCES FROM mix/types.OrderInfo:

  - No HoldSide / TradeSide. Spot has no positions; every order is
    fully described by Side (buy/sell) plus Symbol.
  - No leverage / margin fields.

FIELD CHOICES:

  - OrderID         : Bitget exchange ID. Always non-empty post-accept.
  - ClientOrderID   : pass-through from CreateOrderRequest. The SDK
                      caches OrderID ↔ ClientOrderID so both are
                      populated on every event regardless of what
                      Bitget originally returned on a particular
                      endpoint.
  - Symbol/Side/Type/Force/Status : straight wire mapping using the
                      typed enums in github.com/tonymontanov/go-bitget/v2/types.
  - Quantity / Price / FilledQuantity / AvgFilledPrice : decimal.
  - CumFee          : cumulative trading fee in quote currency.
  - CreatedAtMs / UpdatedAtMs : exchange timestamps; updates after
                      every state transition.
*/

package types

import (
	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// OrderInfo — order lifecycle snapshot for one spot order.
type OrderInfo struct {
	OrderID        string
	ClientOrderID  string
	Symbol         string
	Side           roottypes.SideType
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
