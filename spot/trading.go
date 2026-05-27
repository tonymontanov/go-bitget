/*
FILE: spot/trading.go

DESCRIPTION:
Trading sub-client for the Bitget V2 SPOT profile. M1 ships only the
struct + constructor; M2 wires the place / modify / cancel endpoints
(single + batch).

ENDPOINTS WIRED IN M2 (order matters: parsing helpers and shared
request shapes go first, every public method on TradingClient lands
once those are in):

  - POST /api/v2/spot/trade/place-order
  - POST /api/v2/spot/trade/batch-orders
  - POST /api/v2/spot/trade/cancel-order
  - POST /api/v2/spot/trade/cancel-symbol-order
  - POST /api/v2/spot/trade/cancel-replace-order            (modify)
  - POST /api/v2/spot/trade/batch-cancel-replace-order      (modify batch)
  - POST /api/v2/spot/trade/orderInfo                       (query single)

DIFFERENCES FROM mix.TradingClient:

  - No marginMode / marginCoin / tradeSide / holdSide on the wire.
  - cancel-symbol-order takes only `symbol` — there is no productType
    fan-out (mix has /cancel-all-orders + productType, spot does not).
  - batch-cancel-replace-order is a NATIVE endpoint on spot (mix lacks
    one and the SDK does client-side fan-out instead). M2 will use
    the native endpoint; if Bitget tightens the cap or returns 40404
    on the spot family in the future we will mirror the mix client-
    side fan-out fallback.

The trading client uses bgcommon.RestDoer (via c.c.rest()) and
bgcommon.ParseDecimalOrZero / ParseInt64OrZero for response decoding —
spot has no profile-specific number quirks beyond what FlexString
already handles.
*/

package spot

// TradingClient — REST trading sub-client. Built once per spot.Client
// (see client.go) and safe for concurrent use.
type TradingClient struct {
	c *Client
}

func newTradingClient(c *Client) *TradingClient {
	return &TradingClient{c: c}
}
