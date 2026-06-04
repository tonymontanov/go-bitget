/*
FILE: margin/types/create-order-request.go

DESCRIPTION:
CreateOrderRequest — input to margin.TradingClient.CreateOrder /
CreateBatchOrders.

REQUIRED FIELDS (validated client-side):

  - Symbol      (e.g. "BTCUSDT" — a SPOT instrument)
  - Side        (buy / sell — see roottypes.SideType)
  - OrderType   (limit / market — see roottypes.OrderType)
  - exactly one size, by side (see below)
  - Price > 0   (limit only; ignored for market)

SIZE DENOMINATION — IMPORTANT (mirrors spot, but distinct wire fields):

Bitget V2 margin carries the size in TWO distinct fields and picks the
one that matches the order shape:

  - Limit order        → BaseSize  (left coin, e.g. BTC for BTCUSDT)
  - Market SELL        → BaseSize  (sell N units of base)
  - Market BUY (quirk) → QuoteSize (spend N units of quote, e.g. USDT)

The SDK ships exactly the field the caller populated and validates that
the right one is set for the order shape (limit / market-sell ⇒ BaseSize;
market-buy ⇒ QuoteSize). No base/quote conversion is performed.

OPTIONAL FIELDS:

  - ClientOrderID  client-side identifier; if empty the SDK does NOT
                   auto-generate on create (the desk owns the ID space,
                   same choice as mix / spot).
  - TimeInForce    gtc | ioc | fok | post_only. Sent only for limit
                   orders (Bitget ignores `force` on market orders).
  - LoanType       normal | autoLoan | autoRepay | autoLoanAndRepay.
                   Empty defaults to normal.
  - STPMode        self-trade prevention; empty leaves the venue default.
*/

package types

import (
	"github.com/shopspring/decimal"

	roottypes "github.com/tonymontanov/go-bitget/v2/types"
)

// CreateOrderRequest — parameters for placing one margin order.
type CreateOrderRequest struct {
	Symbol        string
	Side          roottypes.SideType
	OrderType     roottypes.OrderType
	TimeInForce   roottypes.TimeInForceType
	Price         decimal.Decimal
	BaseSize      decimal.Decimal
	QuoteSize     decimal.Decimal
	ClientOrderID string
	LoanType      LoanType
	STPMode       STPMode
}
