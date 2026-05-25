/*
FILE: mix/types/enums.go

DESCRIPTION:
Bitget MIX-specific enumerations. Cross-profile enums (ProductType,
SideType, TradeSide, OrderType, TimeInForceType, OrderStatus,
PositionMode, MarginMode) live in github.com/tonymontanov/go-bitget/v2/types
and are imported directly by the desk and the trading code. We add
here ONLY the values the legacy V2 contracts API uses on top of the
shared set.

CURRENT MIX-SPECIFIC VALUES:

  - HoldSide     : direction of an OPEN position. The shared types
    package already exposes SideType (buy/sell) for the
    direction of a single trade and TradeSide (open/close)
    for hedge-mode position legs. HoldSide is a third axis
    that Bitget reports on every position object — distinct
    from both because in hedge mode a single symbol may have
    a long and a short HoldSide simultaneously.
  - BatchAction  : per-row action selector on
    /api/v2/mix/order/batch-modify-order. Used only by
    ModifyBatchOrders in M2.
*/

package types

// HoldSide — direction of an OPEN position on Bitget MIX. Reported on
// every position event and on every order/fill that affects a position.
// Distinct from types.SideType (trade direction) and types.TradeSide
// (open/close leg) because in hedge mode one symbol can hold long and
// short positions concurrently.
type HoldSide string

const (
	// HoldSideLong — long position.
	HoldSideLong HoldSide = "long"
	// HoldSideShort — short position.
	HoldSideShort HoldSide = "short"
)

// String returns the wire value.
func (h HoldSide) String() string { return string(h) }

// BatchAction — per-row action on /api/v2/mix/order/batch-modify-order.
// Used only by ModifyBatchOrders (M2). The bare ModifyOrder endpoint
// does not need it.
type BatchAction string

const (
	// BatchActionModify — amend an existing order.
	BatchActionModify BatchAction = "modify"
	// BatchActionCancel — cancel an existing order.
	BatchActionCancel BatchAction = "cancel"
)

// String returns the wire value.
func (b BatchAction) String() string { return string(b) }
