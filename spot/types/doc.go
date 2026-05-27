/*
FILE: spot/types/doc.go

DESCRIPTION:
Bitget V2 SPOT type namespace. Holds the request / response structs
that are specific to the spot product (e.g. SymbolInfo, BalanceCoin,
CreateOrderRequest, ModifyOrderRequest, OrderInfo). Shared cross-
profile enums (SideType, OrderType, TimeInForceType, OrderStatus)
live in github.com/tonymontanov/go-bitget/v2/types and are imported
directly by the spot/ package.

POPULATED IN MILESTONES:

  - M2: SymbolInfo, MarketTicker, CreateOrderRequest, ModifyOrderRequest,
        BatchOrderResult.
  - M3: BalanceCoin, AccountInfo, OrderInfo (history schema may
        differ slightly from mix's — spot has no holdSide).
  - M4: WS-side wire frames (mostly internal to spot/ — kept private).
  - M5: private WS row converters (account / orders / fills) →
        consumes types defined in M2/M3.

ARCHITECTURE:

Identical structures shared with mix (timestamps, side enums, order
status) are NOT redefined here — spot/types imports them from the
root types package. Only spot-specific shapes get their own file.
This is the same rule mix/types/ already follows.
*/

package types
