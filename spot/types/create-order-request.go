/*
FILE: spot/types/create-order-request.go

DESCRIPTION:
CreateOrderRequest — input to spot.TradingClient.CreateOrder /
CreateBatchOrders.

REQUIRED FIELDS (validated client-side in M2):

  - Symbol         (e.g. "BTCUSDT")
  - Side           (buy / sell — see roottypes.SideType)
  - OrderType      (limit / market — see roottypes.OrderType)
  - Quantity > 0
  - Price > 0      (limit only; ignored for market)

OPTIONAL FIELDS:

  - ClientOrderID  client-side identifier; if empty, the SDK does NOT
                   auto-generate (mirroring the mix profile choice —
                   the desk owns the ID space).
  - TimeInForce    gtc | ioc | fok | post_only. Defaults to gtc on
                   limit orders and to ioc on market orders.

DIFFERENCES FROM mix.CreateOrderRequest:

  - No TradeSide / ReduceOnly. Spot has no positions, so
    open/close/reduce semantics don't apply.
  - No marginMode / marginCoin pinning at the request level — those
    concepts don't exist on plain spot.

QUANTITY UNIT — IMPORTANT:

Bitget V2 spot uses a SIDE-DEPENDENT denomination for `size`:

  - Limit orders         : Quantity is in BASE coin (e.g. BTC for
                           BTCUSDT). This matches every other venue
                           the desk integrates with.
  - Market SELL          : Quantity is in BASE (sell N units of base).
  - Market BUY (quirk)   : Quantity is in QUOTE coin (USDT for
                           BTCUSDT). Bitget interprets the field as
                           "spend this much USDT to buy at market".
                           If you pass a base-side quantity here you
                           will get an unexpectedly large fill.

The SDK ships Quantity exactly as the caller supplied it, without
auto-conversion — converting between base and quote requires a
reference price the SDK does not own. Callers wiring desk-side adapters
must respect this convention or wrap it themselves.
*/

package types

import (
	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// CreateOrderRequest — parameters for placing one spot order.
type CreateOrderRequest struct {
	Symbol        string
	Side          roottypes.SideType
	OrderType     roottypes.OrderType
	TimeInForce   roottypes.TimeInForceType
	Quantity      decimal.Decimal
	Price         decimal.Decimal
	ClientOrderID string
}
