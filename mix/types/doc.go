/*
Package types holds Bitget MIX-specific domain types — those whose
shape is dictated by the V2 contracts API and therefore cannot live in
the cross-profile root types/* (which is shared by mix/, future spot/
and uta/). The protocol-common types — OrderBookLevel, OrderBookSnapshot,
Candle, Candles, Timeframe, TradeUpdate, KlineUpdate, Balance,
CancelOrderRequest — live in github.com/tonymontanov/go-bitget/v2/types
and are imported here directly when needed.

Layout:

  - enums.go              : MIX enums (HoldSide, PosMode, MarginMode,
    OrderType, Force, OrderStatus, BatchAction, ProductType).
  - symbol-info.go        : instrument specification from
    /api/v2/mix/market/contracts.
  - market-ticker.go      : last/mark/index/funding payload from
    /api/v2/mix/market/ticker.
  - order-info.go         : order state placeholder (M2 fills it).
  - position-info.go      : position state placeholder (M3 fills it).
  - create-order-request.go / modify-order-request.go :
    request structs; M1 ships the shape, M2 wires them.
  - batch-result.go       : per-row success/error wrapper used by
    /batch-place-order, /batch-modify-order,
    /batch-cancel-orders.

These types are exposed verbatim to callers (no interface{} payloads,
no map[string]string).
*/
package types
