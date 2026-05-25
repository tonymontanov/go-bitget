/*
FILE: mix/types/create-order-request.go

DESCRIPTION:
CreateOrderRequest — input to TradingClient.CreateOrder. M1 ships the
shape; M2 wires the actual REST call.

REQUIRED FIELDS (validated client-side in M2):

  - Symbol          (e.g. "BTCUSDT")
  - Side            (buy / sell)
  - OrderType       (limit / market)
  - Quantity > 0
  - Price > 0       (limit only; ignored for market)

OPTIONAL FIELDS:

  - ClientOrderID   client-side identifier; if empty, the SDK does NOT
    auto-generate (mirroring the OKX/Bybit profile choice — the desk
    owns the ID space).
  - TimeInForce     gtc | ioc | fok | post_only. Defaults to gtc for
    limit and ioc for market on the wire.
  - TradeSide       open / close, for hedge mode only. Omitted in
    one-way mode (default for v1.0).
  - ReduceOnly      Bitget honours this on one-way mode; in hedge mode
    Bitget infers it from TradeSide instead.
*/

package types

import (
	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// CreateOrderRequest — parameters for placing one MIX order.
type CreateOrderRequest struct {
	Symbol        string
	Side          roottypes.SideType
	OrderType     roottypes.OrderType
	TimeInForce   roottypes.TimeInForceType
	TradeSide     roottypes.TradeSide
	Quantity      decimal.Decimal
	Price         decimal.Decimal
	ClientOrderID string
	ReduceOnly    bool
}
